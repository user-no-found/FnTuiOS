package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"fntuios/internal/config"
	"github.com/user-no-found/FnPackKit/runtime/identity"
)

type containerFixture struct {
	native        bool
	gatewaySocket string
	t             *testing.T
	root          string
	cfg           config.Config
	users         []identity.User
	process       *child
	client        *http.Client
	checks        []string
	transcript    string
}

// TestContainerRuntime uses two real OS identities and the shipped binaries.
// Account creation is allowed only in an explicitly selected disposable container.
func TestContainerRuntime(t *testing.T) {
	if os.Getenv("FNTUIOS_E2E_CONTAINER") != "1" {
		t.Skip("requires isolated container; see docs/runtime.md")
	}
	if os.Geteuid() != 0 {
		t.Fatal("container test requires root inside the container")
	}
	if _, err := os.Stat("/.dockerenv"); err != nil {
		t.Fatal("refusing account fixture creation outside Docker")
	}
	binDir := os.Getenv("FNTUIOS_E2E_BIN_DIR")
	if binDir == "" {
		t.Fatal("FNTUIOS_E2E_BIN_DIR is required")
	}
	root, err := os.MkdirTemp("/tmp", "fntuios-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	if err := os.Chmod(root, 0711); err != nil {
		t.Fatal(err)
	}
	fixture := &containerFixture{t: t, root: root}
	fixture.prepare(binDir)
	defer fixture.stop()
	fixture.start()
	fixture.exercise()
	fixture.stop()
	fixture.start()
	for index, user := range fixture.users {
		fixture.wantStatus(user, "/app/tuios/", nil, 200)
		capture := fixture.cli(user, "capture-pane", "-s", "web", "--scrollback")
		if !strings.Contains(capture, fmt.Sprintf("FN_USER_%d", index)) {
			t.Fatalf("user %d history missing after restart: %s", index, capture)
		}
	}
	fixture.check("both user session histories survive ordered daemon/web restart")
	fixture.stop()
	fixture.cfg.MaxUsers = 1
	data, _ := json.Marshal(fixture.cfg)
	if err := os.WriteFile(filepath.Join(fixture.root, "gateway.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	fixture.start()
	fixture.wantStatus(fixture.users[0], "/app/tuios/", nil, 200)
	fixture.wantStatus(fixture.users[1], "/app/tuios/", nil, 503)
	fixture.check("editable max_users takes effect after restart without rebuilding")
	fixture.stop()
	fixture.failedStartup()
	report := os.Getenv("FNTUIOS_E2E_REPORT")
	if report != "" {
		data, _ := json.MarshalIndent(map[string]any{"checks": fixture.checks, "transcript": fixture.transcript, "transport": "Unix socket HTTP and WebSocket", "identity": "two NSS users, root broker, unprivileged gateway"}, "", "  ")
		if err := os.WriteFile(report, data, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(report, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func (fixture *containerFixture) failedStartup() {
	webPath := filepath.Join(fixture.cfg.AppDir, "bin", "tuios-web")
	if err := os.Rename(webPath, webPath+".saved"); err != nil {
		fixture.t.Fatal(err)
	}
	defer os.Rename(webPath+".saved", webPath)
	data, err := os.ReadFile("/bin/false")
	if err != nil {
		fixture.t.Fatal(err)
	}
	if err := os.WriteFile(webPath, data, 0755); err != nil {
		fixture.t.Fatal(err)
	}
	fixture.start()
	fixture.wantStatus(fixture.users[0], "/app/tuios/", nil, 503)
	socket := filepath.Join(fixture.cfg.RunDir, strconv.Itoa(int(fixture.users[0].UID)), "tuios", "tuios.sock")
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		fixture.t.Fatalf("daemon socket survived failed Web startup: %v", err)
	}
	fixture.stop()
	fixture.check("failed Web startup cleans up its separate daemon before reporting unavailable")
}

func (fixture *containerFixture) prepare(binDir string) {
	t := fixture.t
	appDir := filepath.Join(fixture.root, "app")
	if err := os.MkdirAll(filepath.Join(appDir, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tuios", "tuios-web", "fntuios"} {
		data, err := os.ReadFile(filepath.Join(binDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(appDir, "bin", name), data, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for index, name := range []string{"fnalice", "fnbob"} {
		uid := 45000 + index
		home := filepath.Join(fixture.root, name)
		runFixtureCommand(t, "/usr/sbin/useradd", "-m", "-d", home, "-u", strconv.Itoa(uid), "-U", "-s", "/usr/sbin/nologin", name)
	}
	runFixtureCommand(t, "/usr/sbin/groupadd", "-g", "45002", "fnshared")
	runFixtureCommand(t, "/usr/sbin/usermod", "-aG", "fnshared", "fnalice")
	runFixtureCommand(t, "/usr/sbin/useradd", "-r", "-U", "-s", "/usr/sbin/nologin", "fngateway")
	for index, name := range []string{"fnalice", "fnbob"} {
		user, err := identity.Resolve(identity.Request{UID: uint32(45000 + index), Name: name})
		if err != nil {
			t.Fatal(err)
		}
		fixture.users = append(fixture.users, user)
	}
	fixture.cfg = config.Config{AppDir: appDir, StateDir: filepath.Join(fixture.root, "state"), RunDir: filepath.Join(fixture.root, "run"), Prefix: "/app/tuios", GatewayAccount: "fngateway", TrustedGatewayAccounts: []string{"root"}, AllowedHosts: []string{"nas.example"}, MaxUsers: 2, MaxConnectionsPerUser: 3, ConnectionLifetimeSeconds: 60, StartupTimeoutSeconds: 30, ShutdownTimeoutSeconds: 30, UserPath: "/usr/bin:/bin", Language: "C.UTF-8", TerminalShell: "/bin/bash"}
	fixture.cfg.ExternalOriginPorts = map[string][]uint16{"http": {5666}, "https": {5667}}
	data, _ := json.Marshal(fixture.cfg)
	if err := os.WriteFile(filepath.Join(fixture.root, "gateway.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	fixture.client = &http.Client{Transport: &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(appDir, "app.sock"))
	}}, Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (fixture *containerFixture) start() {
	cmd := exec.Command(filepath.Join(fixture.cfg.AppDir, "bin", "fntuios"), "serve", "--config", filepath.Join(fixture.root, "gateway.json"))
	logFile, err := os.OpenFile(filepath.Join(fixture.root, "supervisor.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		fixture.t.Fatal(err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = []string{"PATH=/usr/bin:/bin", "INHERITED_SECRET=must-not-reach-user"}
	fixture.process, err = startChild(cmd)
	logFile.Close()
	if err != nil {
		fixture.t.Fatal(err)
	}
	fixture.t.Cleanup(func() {
		if fixture.t.Failed() {
			data, _ := os.ReadFile(filepath.Join(fixture.root, "supervisor.log"))
			fixture.t.Logf("supervisor log: %s", data)
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !fixture.process.Alive() {
			fixture.t.Fatalf("supervisor exited: %v", fixture.process.Result())
		}
		response, err := fixture.client.Get("http://nas.example/app/tuios/")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 403 {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	fixture.t.Fatal("supervisor did not become ready")
}

func (fixture *containerFixture) stop() {
	if fixture.process == nil {
		return
	}
	if err := fixture.process.Terminate(40 * time.Second); err != nil {
		fixture.t.Errorf("stop: %v", err)
	}
	fixture.process = nil
	fixture.client.CloseIdleConnections()
}

func (fixture *containerFixture) wantStatus(user identity.User, path string, mutate func(*http.Request), status int) string {
	fixture.t.Helper()
	request, _ := http.NewRequest("GET", "http://nas.example"+path, nil)
	request.Header.Set("X-Trim-Userid", strconv.FormatUint(uint64(user.UID), 10))
	request.Header.Set("X-Trim-Username", user.Name)
	if mutate != nil {
		mutate(request)
	}
	response, err := fixture.client.Do(request)
	if err != nil {
		fixture.t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != status {
		fixture.t.Fatalf("%s status=%d want=%d body=%s", path, response.StatusCode, status, body)
	}
	return string(body)
}

func (fixture *containerFixture) cli(user identity.User, args ...string) string {
	fixture.t.Helper()
	output, err := fixture.cliCommand(user, args...).CombinedOutput()
	if err != nil {
		fixture.t.Fatalf("tuios %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func (fixture *containerFixture) cliCommand(user identity.User, args ...string) *exec.Cmd {
	cmd := exec.Command(filepath.Join(fixture.cfg.AppDir, "bin", "tuios"), args...)
	spec := WorkerSpec{Config: fixture.cfg, User: user, StateDir: filepath.Join(fixture.cfg.StateDir, strconv.FormatUint(uint64(user.UID), 10)), RunDir: filepath.Join(fixture.cfg.RunDir, strconv.FormatUint(uint64(user.UID), 10))}
	cmd.Env = userEnvironment(spec)
	cmd.Dir = user.Home
	if !fixture.native {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: user.UID, Gid: user.GID, Groups: user.Groups}}
	}
	return cmd
}

func (fixture *containerFixture) check(name string) {
	fixture.checks = append(fixture.checks, name)
	fixture.t.Log("PASS", name)
}

func runFixtureCommand(t *testing.T, program string, args ...string) {
	t.Helper()
	if data, err := exec.Command(program, args...).CombinedOutput(); err != nil {
		t.Fatalf("fixture %s: %v %s", program, err, data)
	}
}
