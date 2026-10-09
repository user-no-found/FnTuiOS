package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"fntuios/internal/config"
	"fntuios/internal/gateway"
	"github.com/user-no-found/FnPackKit/runtime/identity"
)

// WorkerSpec is sent only by the supervisor through the worker's inherited stdin.
type WorkerSpec struct {
	Config   config.Config
	User     identity.User
	StateDir string
	RunDir   string
}

// RunWorker starts the user's daemon before web, and saves the daemon before stopping web.
func RunWorker(ctx context.Context, spec WorkerSpec, report *os.File) error {
	if os.Geteuid() == 0 || uint32(os.Geteuid()) != spec.User.UID || uint32(os.Getegid()) != spec.User.GID {
		return errors.New("worker must already have the selected nonroot identity")
	}
	syscall.Umask(0077)
	lock, err := os.OpenFile(filepath.Join(spec.RunDir, "worker.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("a user runtime is already active")
	}
	for _, directory := range []string{"config", "data", "state", "cache"} {
		if err := os.MkdirAll(filepath.Join(spec.StateDir, directory), 0700); err != nil {
			return err
		}
	}
	logFile, err := os.OpenFile(filepath.Join(spec.StateDir, "runtime.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	environment := userEnvironment(spec)
	launch := func(binary string, args ...string) (*child, error) {
		cmd := exec.Command(filepath.Join(spec.Config.AppDir, "bin", binary), args...)
		cmd.Env = environment
		cmd.Dir = spec.User.Home
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM, Setpgid: true}
		return startChild(cmd)
	}
	daemon, err := launch("tuios", "daemon")
	if err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	var web *child
	defer func() {
		// A timed-out save keeps this worker alive and its parent informed.
		// Killing web while the daemon is saving can discard session state.
		for daemon.Alive() {
			_ = daemon.Terminate(time.Duration(spec.Config.ShutdownTimeoutSeconds) * time.Second)
		}
		if web != nil {
			_ = web.Terminate(time.Duration(spec.Config.ShutdownTimeoutSeconds) * time.Second)
		}
	}()
	startupCtx, cancel := context.WithTimeout(ctx, time.Duration(spec.Config.StartupTimeoutSeconds)*time.Second)
	defer cancel()
	socket := filepath.Join(spec.RunDir, "tuios", "tuios.sock")
	if err := waitReady(startupCtx, daemon, func() bool { info, err := os.Stat(socket); return err == nil && info.Mode()&os.ModeSocket != 0 }); err != nil {
		return fmt.Errorf("daemon readiness: %w", err)
	}
	passwordBytes := make([]byte, 32)
	if _, err := rand.Read(passwordBytes); err != nil {
		return err
	}
	password := base64.RawURLEncoding.EncodeToString(passwordBytes)
	passwordFile, err := os.CreateTemp(spec.RunDir, ".web-password-")
	if err != nil {
		return err
	}
	defer os.Remove(passwordFile.Name())
	if _, err := passwordFile.WriteString(password + "\n"); err != nil {
		passwordFile.Close()
		return err
	}
	if err := passwordFile.Close(); err != nil {
		return err
	}
	address, err := unusedLoopbackAddress()
	if err != nil {
		return err
	}
	_, port, _ := net.SplitHostPort(address)
	web, err = launch("tuios-web", "--host", "127.0.0.1", "--port", port, "--user", "fntuios", "--password-file", passwordFile.Name(), "--max-connections", strconv.Itoa(spec.Config.MaxConnectionsPerUser))
	if err != nil {
		return fmt.Errorf("start web: %w", err)
	}
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	if err := waitReady(startupCtx, web, func() bool {
		request, _ := http.NewRequest("GET", "http://"+address+"/", nil)
		request.SetBasicAuth("fntuios", password)
		response, err := client.Do(request)
		if err != nil {
			return false
		}
		response.Body.Close()
		return response.StatusCode == http.StatusOK && daemon.Alive()
	}); err != nil {
		return fmt.Errorf("web readiness: %w", err)
	}
	if err := json.NewEncoder(report).Encode(gateway.Endpoint{UID: spec.User.UID, Address: address, Password: password}); err != nil {
		return err
	}
	report.Close()
	select {
	case <-ctx.Done():
		return nil
	case <-daemon.Done():
		return errors.New("user daemon stopped unexpectedly")
	case <-web.Done():
		return errors.New("user web stopped unexpectedly")
	}
}

func userEnvironment(spec WorkerSpec) []string {
	return []string{
		"HOME=" + spec.User.Home, "USER=" + spec.User.Name, "LOGNAME=" + spec.User.Name, "SHELL=" + spec.Config.TerminalShell,
		"PATH=" + spec.Config.UserPath, "LANG=" + spec.Config.Language, "LC_ALL=" + spec.Config.Language,
		"XDG_CONFIG_HOME=" + filepath.Join(spec.StateDir, "config"), "XDG_DATA_HOME=" + filepath.Join(spec.StateDir, "data"),
		"XDG_STATE_HOME=" + filepath.Join(spec.StateDir, "state"), "XDG_CACHE_HOME=" + filepath.Join(spec.StateDir, "cache"),
		"XDG_RUNTIME_DIR=" + spec.RunDir, "TMPDIR=" + spec.RunDir, "TERM=xterm-256color", "COLORTERM=truecolor",
	}
}

func waitReady(ctx context.Context, process *child, ready func() bool) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !process.Alive() {
			return errors.New("process exited before readiness")
		}
		if ready() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-process.Done():
			return errors.New("process exited before readiness")
		case <-ticker.C:
		}
	}
}

func unusedLoopbackAddress() (string, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	address := listener.Addr().String()
	return address, listener.Close()
}
