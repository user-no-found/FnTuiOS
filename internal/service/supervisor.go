package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"fntuios/internal/config"
	"fntuios/internal/gateway"
	"github.com/user-no-found/FnPackKit/runtime/identity"
	"github.com/user-no-found/FnPackKit/runtime/process"
	"github.com/user-no-found/FnPackKit/runtime/securefs"
)

// Serve owns privileged preparation and the identity-bound launcher/control channel.
func Serve(ctx context.Context, configPath string) error {
	if os.Geteuid() != 0 {
		return errors.New("serve requires the package's privileged service launcher")
	}
	syscall.Umask(0077)
	configPath, err := securefs.RootPath(configPath)
	if err != nil {
		return err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	cfg.AppDir, err = securefs.RootPath(cfg.AppDir)
	if err != nil {
		return err
	}
	cfg.TerminalShell, err = securefs.RootPath(cfg.TerminalShell)
	if err != nil {
		return fmt.Errorf("terminal_shell: %w", err)
	}
	if info, err := os.Stat(cfg.TerminalShell); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return errors.New("terminal_shell must be an executable file")
	}
	for _, name := range []string{"fntuios", "tuios", "tuios-web"} {
		if _, err := executablePath(cfg.AppDir, name); err != nil {
			return err
		}
	}
	cfg.StateDir, err = securefs.RootDirectory(cfg.StateDir, 0711)
	if err != nil {
		return err
	}
	cfg.RunDir, err = securefs.RootDirectory(cfg.RunDir, 0711)
	if err != nil {
		return err
	}
	account, err := identity.SystemAccount(cfg.GatewayAccount)
	if err != nil || account.UID == 0 {
		return errors.New("gateway_account must resolve to a nonroot account")
	}
	options := gateway.Options{Config: cfg}
	socketGID := uint32(0)
	for _, name := range cfg.TrustedGatewayAccounts {
		peer, err := identity.SystemAccount(name)
		if err != nil {
			return err
		}
		if peer.UID >= 1000 || peer.UID == account.UID {
			return errors.New("trusted gateway peers must be system identities distinct from the application")
		}
		if peer.UID != 0 {
			if socketGID != 0 && socketGID != peer.GID {
				return errors.New("nonroot gateway peers must share one socket group")
			}
			socketGID = peer.GID
		}
		options.TrustedUIDs = append(options.TrustedUIDs, peer.UID)
	}
	lock, err := lockSupervisor(cfg.RunDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	socketPath := filepath.Join(cfg.AppDir, "app.sock")
	listener, err := gatewaySocket(socketPath, socketGID)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(socketPath)
	listenerFile, err := listener.File()
	if err != nil {
		return err
	}
	defer listenerFile.Close()
	pair, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return err
	}
	parentFile := os.NewFile(uintptr(pair[0]), "broker-parent")
	defer parentFile.Close()
	childFile := os.NewFile(uintptr(pair[1]), "broker-child")
	defer childFile.Close()
	connection, err := net.FileConn(parentFile)
	if err != nil {
		return err
	}
	defer connection.Close()
	self, err := executablePath(cfg.AppDir, "fntuios")
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(options)
	if err != nil {
		return err
	}
	cmd := exec.Command(self, "gateway")
	cmd.Stdin = bytes.NewReader(encoded)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	cmd.Dir = "/"
	cmd.ExtraFiles = []*os.File{listenerFile, childFile}
	cmd.SysProcAttr, err = process.Credential(account.UID, account.GID, []uint32{})
	if err != nil {
		return err
	}
	child, err := startChild(cmd)
	if err != nil {
		return err
	}
	childFile.Close()
	listenerFile.Close()
	listener.Close()
	manager := &broker{config: cfg, self: self, gatewayUID: account.UID, instances: make(map[uint32]*instance)}
	brokerDone := make(chan error, 1)
	go func() { brokerDone <- manager.serve(connection) }()
	select {
	case <-ctx.Done():
	case <-child.Done():
		err = fmt.Errorf("gateway exited: %w", child.Result())
	case brokerErr := <-brokerDone:
		err = fmt.Errorf("gateway control channel closed: %v", brokerErr)
	}
	_ = child.Terminate(5 * time.Second)
	connection.Close()
	manager.stop()
	return err
}

func gatewaySocket(path string, gid uint32) (*net.UnixListener, error) {
	if info, err := os.Lstat(path); err == nil {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || info.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("gateway socket path is not a root-owned socket")
		}
		conn, err := net.DialTimeout("unix", path, time.Second)
		if err == nil {
			conn.Close()
			return nil, errors.New("gateway socket is already active")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	listener.SetUnlinkOnClose(false)
	mode := os.FileMode(0600)
	if gid != 0 {
		mode = 0660
	}
	if err := os.Chown(path, 0, int(gid)); err != nil {
		listener.Close()
		return nil, err
	}
	if err := os.Chmod(path, mode); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}
