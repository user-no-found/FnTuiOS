package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareStateRequiresRootBeforeReadingConfig(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires an ordinary caller to verify the privilege boundary")
	}
	if err := PrepareState(filepath.Join(t.TempDir(), "absent.json")); err == nil || !strings.Contains(err.Error(), "requires root") {
		t.Fatalf("expected root boundary before configuration access, got %v", err)
	}
}

func TestSupervisorLockRejectsConcurrentOwner(t *testing.T) {
	root := t.TempDir()
	first, err := lockSupervisor(root)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := lockSupervisor(root); err == nil {
		second.Close()
		t.Fatal("active supervisor did not exclude lifecycle preparation")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := lockSupervisor(root)
	if err != nil {
		t.Fatalf("released supervisor lock remained unavailable: %v", err)
	}
	second.Close()
}

func TestSupervisorLockRejectsLinkedControlFiles(t *testing.T) {
	for _, kind := range []string{"symbolic", "hard"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "sentinel")
			if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
				t.Fatal(err)
			}
			link := os.Link
			if kind == "symbolic" {
				link = os.Symlink
			}
			if err := link(target, filepath.Join(root, "supervisor.lock")); err != nil {
				t.Fatal(err)
			}
			if lock, err := lockSupervisor(root); err == nil {
				lock.Close()
				t.Fatal("linked supervisor control file was accepted")
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "unchanged" {
				t.Fatal("rejected control link changed its target")
			}
		})
	}
}
