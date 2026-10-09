package service

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"

	"fntuios/internal/config"
	"github.com/user-no-found/FnPackKit/runtime/identity"
	"github.com/user-no-found/FnPackKit/runtime/securefs"
)

// PrepareState restores package-managed user ownership while the service is offline.
// It does not inspect session contents or change file modes.
func PrepareState(configPath string) error {
	if os.Geteuid() != 0 {
		return errors.New("prepare-state requires root")
	}
	configPath, err := securefs.RootPath(configPath)
	if err != nil {
		return err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	runDir, err := securefs.RootPath(cfg.RunDir)
	if err != nil {
		return err
	}
	lock, err := lockSupervisor(runDir)
	if err != nil {
		return fmt.Errorf("prepare-state requires an offline supervisor: %w", err)
	}
	defer lock.Close()
	account, err := identity.SystemAccount(cfg.GatewayAccount)
	if err != nil || account.UID == 0 {
		return errors.New("gateway_account must resolve to a nonroot account")
	}
	report, err := securefs.RestoreUserState(cfg.StateDir, securefs.RestoreOptions{
		ExcludedUIDs: []uint32{account.UID}, MaxEntries: cfg.StateRestoreMaxEntries, MaxDepth: cfg.StateRestoreMaxDepth,
	})
	if err != nil {
		return fmt.Errorf("prepare user state: %w", err)
	}
	log.Printf("user state prepared users=%d entries=%d existing=%d symlinks_skipped=%d ignored=%d",
		report.UsersRestored, report.EntriesRestored, report.UserOwnedTrees, report.SymlinksSkipped, report.IgnoredEntries)
	return nil
}

// lockSupervisor excludes both a running supervisor and an offline state repair.
// Callers validate the parent directory before using this shared control file.
func lockSupervisor(runDir string) (*os.File, error) {
	lock, err := os.OpenFile(filepath.Join(runDir, "supervisor.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	info, err := lock.Stat()
	if err == nil {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 || info.Mode().Perm() != 0600 {
			err = errors.New("unsafe supervisor lock file")
		}
	}
	if err == nil {
		if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
			err = errors.New("supervisor is already running or state is being prepared")
		}
	}
	if err != nil {
		lock.Close()
		return nil, err
	}
	return lock, nil
}
