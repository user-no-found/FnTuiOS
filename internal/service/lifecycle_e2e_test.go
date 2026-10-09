package service

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/user-no-found/FnPackKit/runtime/identity"
)

// TestContainerLifecycle exercises package callbacks starting from fnOS unpack permissions.
func TestContainerLifecycle(t *testing.T) {
	if os.Getenv("FNTUIOS_E2E_CONTAINER") != "1" || os.Getenv("FNTUIOS_E2E_STAGE_DIR") == "" {
		t.Skip("requires container and extracted package stage")
	}
	if os.Geteuid() != 0 {
		t.Fatal("requires container root")
	}
	if _, err := os.Stat("/.dockerenv"); err != nil {
		t.Fatal("refusing fixture outside Docker")
	}
	stage := os.Getenv("FNTUIOS_E2E_STAGE_DIR")
	root, err := os.MkdirTemp("/tmp", "fntuios-lifecycle-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	if err := os.Chmod(root, 0711); err != nil {
		t.Fatal(err)
	}
	runFixtureCommand(t, "/usr/sbin/useradd", "-r", "-U", "-s", "/usr/sbin/nologin", "tuios")
	account, err := identity.SystemAccount("tuios")
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(root, "payload")
	if err := os.CopyFS(app, os.DirFS(filepath.Join(stage, "app"))); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(app, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	var sums strings.Builder
	for _, name := range []string{"tuios", "tuios-web", "fntuios"} {
		data, err := os.ReadFile(filepath.Join(os.Getenv("FNTUIOS_E2E_BIN_DIR"), name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(app, "bin", name), data, 0775); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256(data), name)
	}
	if err := os.WriteFile(filepath.Join(app, "bin", "SHA256SUMS"), []byte(sums.String()), 0664); err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(app, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unexpected fixture symlink %s", path)
		}
		if err := os.Chown(path, int(account.UID), int(account.GID)); err != nil {
			return err
		}
		return os.Chmod(path, 0775)
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"etc", "var"} {
		path := filepath.Join(root, name)
		if err := os.Mkdir(path, 0770); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(path, int(account.UID), int(account.GID)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(root, "etc"), 0700); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(root, "var", "legacy-session")
	if err := os.WriteFile(legacy, []byte("legacy-private-session"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(legacy, int(account.UID), int(account.GID)); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "target")
	if err := os.Symlink(app, link); err != nil {
		t.Fatal(err)
	}
	environment := []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "TRIM_APPDEST=" + link, "TRIM_PKGETC=" + filepath.Join(root, "etc"), "TRIM_PKGVAR=" + filepath.Join(root, "var")}
	if os.Getenv("FNTUIOS_E2E_TRACE") == "1" {
		environment = append(environment, "SHELLOPTS=xtrace")
	}
	run := func(name string, args ...string) ([]byte, error) {
		cmd := exec.Command(filepath.Join(stage, "cmd", name), args...)
		cmd.Env = environment
		return cmd.CombinedOutput()
	}
	want := func(name string, args ...string) {
		t.Helper()
		if output, err := run(name, args...); err != nil {
			log, _ := os.ReadFile(filepath.Join(root, "var", "fntuios", "service.log"))
			pid, _ := os.ReadFile("/run/fntuios/supervisor.pid")
			var executable string
			if fields := strings.Fields(string(pid)); len(fields) > 0 {
				executable, _ = os.Readlink("/proc/" + fields[0] + "/exe")
			}
			healthErr := Health(filepath.Join(root, "etc", "gateway.json"))
			t.Fatalf("%s %v: %v %s\n%s\npid=%s exe=%s health=%v", name, args, err, output, log, pid, executable, healthErr)
		}
	}
	want("install_callback")
	t.Cleanup(func() { run("main", "stop") })
	want("main", "start")
	want("main", "status")
	before, err := os.ReadFile("/run/fntuios/supervisor.pid")
	if err != nil {
		t.Fatal(err)
	}
	want("main", "start")
	after, err := os.ReadFile("/run/fntuios/supervisor.pid")
	if err != nil || string(before) != string(after) {
		t.Fatal("repeated start changed supervisor identity")
	}
	want("main", "stop")
	legacyData, err := os.ReadFile(legacy)
	if err != nil || string(legacyData) != "legacy-private-session" {
		t.Fatal("legacy shared data changed")
	}
	legacyInfo, err := os.Stat(legacy)
	if err != nil || legacyInfo.Sys().(*syscall.Stat_t).Uid != account.UID {
		t.Fatal("legacy data ownership changed")
	}
	if _, err := run("main", "status"); err == nil {
		t.Fatal("stopped package reports running")
	}
	// A stale root socket is safely replaced; it must not count as readiness.
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(app, "app.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	listener.Close()
	want("main", "start")
	want("main", "status")
	want("main", "stop")
	configPath := filepath.Join(root, "etc", "gateway.json")
	goodConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var invalid map[string]any
	if err := json.Unmarshal(goodConfig, &invalid); err != nil {
		t.Fatal(err)
	}
	invalid["gateway_account"] = "missing_fixture_gateway"
	invalidData, _ := json.Marshal(invalid)
	if err := os.WriteFile(configPath, invalidData, 0600); err != nil {
		t.Fatal(err)
	}
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(app, "app.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	stale.Close()
	if _, err := run("main", "start"); err == nil {
		t.Fatal("invalid account with stale socket reported started")
	}
	if _, err := run("main", "status"); err == nil {
		t.Fatal("invalid account with stale socket reported running")
	}
	if err := os.WriteFile(configPath, goodConfig, 0600); err != nil {
		t.Fatal(err)
	}
	// A correct start-time with the wrong executable must not cause a kill.
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(os.Getpid()) + "/stat")
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(stat)[strings.LastIndex(string(stat), ")")+1:])
	record := fmt.Sprintf("%d %s\n", os.Getpid(), fields[19])
	if err := os.WriteFile("/run/fntuios/supervisor.pid", []byte(record), 0600); err != nil {
		t.Fatal(err)
	}
	want("main", "stop")
	// Installer policy cannot chown or rewrite a symlink target.
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, "sentinel")
	if err := os.WriteFile(sentinel, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, configPath); err != nil {
		t.Fatal(err)
	}
	if _, err := run("install_callback"); err == nil {
		t.Fatal("installer accepted symlink configuration")
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "untouched" {
		t.Fatal("installer changed symlink target")
	}
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, goodConfig, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(configPath, int(account.UID), int(account.GID)); err != nil {
		t.Fatal(err)
	}
	if _, err := run("install_callback"); err == nil {
		t.Fatal("installer adopted package-user-owned gateway policy")
	}
	info, err := os.Stat(configPath)
	if err != nil || info.Sys().(*syscall.Stat_t).Uid != account.UID {
		t.Fatal("rejected policy was silently adopted")
	}
	if err := os.Chown(configPath, 0, 0); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, "var", "fntuios")
	logPath := filepath.Join(stateDir, "service.log")
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(stateDir, int(account.UID), int(account.GID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stateDir, 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, logPath); err != nil {
		t.Fatal(err)
	}
	if _, err := run("install_callback"); err == nil {
		t.Fatal("installer adopted writable state with symlink log")
	}
	if _, err := run("main", "start"); err == nil {
		t.Fatal("start accepted writable state with symlink log")
	}
	data, err = os.ReadFile(sentinel)
	if err != nil || string(data) != "untouched" {
		t.Fatal("malicious log symlink changed its root target")
	}
	checks := []string{"fnOS package-account ownership and 0775 payload secured by actual install callback", "canonical target symlink supported", "actual start/status/stop and idempotent start", "stale socket does not masquerade as running", "wrong executable PID is not signalled", "symlink configuration rejected without altering target", "legacy session data and ownership preserved", "invalid gateway account plus stale socket cannot report started/running", "package-user-owned policy is not silently adopted", "writable state and symlink log cannot alter a root-owned sentinel"}
	for _, check := range checks {
		t.Log("PASS", check)
	}
	if report := os.Getenv("FNTUIOS_E2E_REPORT"); report != "" {
		data, _ := json.MarshalIndent(map[string]any{"checks": checks}, "", "  ")
		if err := os.WriteFile(report, data, 0644); err != nil {
			t.Fatal(err)
		}
		os.Chmod(report, 0644)
	}
}
