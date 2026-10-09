package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"fntuios/internal/config"
	"fntuios/internal/gateway"
	"github.com/user-no-found/FnPackKit/runtime/identity"
)

// nativeFixture uses the caller's UID and owns every runtime path it creates.
// It tests the shipped worker and gateway; it does not emulate privilege dropping.
type nativeFixture struct {
	*containerFixture
	worker   *child
	endpoint gateway.Endpoint
}

func newNativeFixture(t *testing.T) *nativeFixture {
	t.Helper()
	if os.Getenv("FNTUIOS_E2E_NATIVE") != "1" {
		t.Skip("requires explicit native package fixture")
	}
	if os.Geteuid() == 0 {
		t.Fatal("native fixture must run as an ordinary user")
	}
	binDir, err := filepath.Abs(os.Getenv("FNTUIOS_E2E_BIN_DIR"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tuios", "tuios-web", "fntuios"} {
		if stat, err := os.Stat(filepath.Join(binDir, name)); err != nil || !stat.Mode().IsRegular() {
			t.Fatalf("missing package binary %s", name)
		}
	}
	root, err := os.MkdirTemp("", "n-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	person := identity.User{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid()), Name: account.Username, Home: home, Shell: "/bin/bash"}
	cfg := config.Config{AppDir: filepath.Dir(binDir), StateDir: filepath.Join(root, "state"), RunDir: filepath.Join(root, "r"), Prefix: "/app/tuios", MaxUsers: 1, MaxConnectionsPerUser: 4, ConnectionLifetimeSeconds: 600, StartupTimeoutSeconds: 30, ShutdownTimeoutSeconds: 30, UserPath: "/usr/bin:/bin", Language: "C.UTF-8", TerminalShell: "/bin/bash"}
	for _, parent := range []string{cfg.StateDir, cfg.RunDir} {
		if err := os.MkdirAll(filepath.Join(parent, strconv.Itoa(int(person.UID))), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return &nativeFixture{containerFixture: &containerFixture{t: t, root: root, cfg: cfg, users: []identity.User{person}, native: true}}
}

func (fixture *nativeFixture) startWorker() {
	t := fixture.t
	person := fixture.users[0]
	spec := WorkerSpec{Config: fixture.cfg, User: person, StateDir: filepath.Join(fixture.cfg.StateDir, strconv.Itoa(int(person.UID))), RunDir: filepath.Join(fixture.cfg.RunDir, strconv.Itoa(int(person.UID)))}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	logFile, err := os.OpenFile(filepath.Join(fixture.root, "worker.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	cmd := exec.Command(filepath.Join(fixture.cfg.AppDir, "bin/fntuios"), "worker")
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	cmd.Stdin = bytes.NewReader(data)
	cmd.ExtraFiles = []*os.File{writer}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = []string{"PATH=/usr/bin:/bin", "INHERITED_SECRET=must-not-reach-user"}
	fixture.worker, err = startChild(cmd)
	writer.Close()
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.SetReadDeadline(time.Now().Add(45 * time.Second))
	if err := json.NewDecoder(reader).Decode(&fixture.endpoint); err != nil {
		log, _ := os.ReadFile(filepath.Join(fixture.root, "worker.log"))
		fixture.stopWorker()
		t.Fatalf("worker readiness: %v\n%s", err, log)
	}
}

func (fixture *nativeFixture) stopWorker() {
	if fixture.worker == nil {
		return
	}
	if err := fixture.worker.Terminate(40 * time.Second); err != nil {
		fixture.t.Errorf("worker stop: %v", err)
		return
	}
	fixture.worker = nil
}

// TestNativeWorker verifies actual packaged processes, UID, auth, and saved state.
func TestNativeWorker(t *testing.T) {
	fixture := newNativeFixture(t)
	fixture.startWorker()
	defer fixture.stopWorker()
	client := &http.Client{Timeout: 10 * time.Second}
	request, _ := http.NewRequest("GET", "http://"+fixture.endpoint.Address+"/", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatalf("unauthenticated backend status %d", response.StatusCode)
	}
	request.SetBasicAuth("fntuios", fixture.endpoint.Password)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("authenticated backend status %d", response.StatusCode)
	}
	person := fixture.users[0]
	fixture.cli(person, "new", "native-check", "--detach")
	fixture.cli(person, "new-window", "native", "-s", "native-check", "--", "/bin/bash", "-c", "printf 'NATIVE_UID_%s_SECRET_%s\\n' \"$(id -u)\" \"${INHERITED_SECRET-unset}\"; sleep 300")
	want := fmt.Sprintf("NATIVE_UID_%d_SECRET_unset", person.UID)
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(fixture.cli(person, "capture-pane", "-s", "native-check", "--scrollback"), want) {
		if time.Now().After(deadline) {
			t.Fatal("terminal UID/environment check timed out")
		}
		time.Sleep(100 * time.Millisecond)
	}
	fixture.stopWorker()
	fixture.startWorker()
	if !strings.Contains(fixture.cli(person, "capture-pane", "-s", "native-check", "--scrollback"), want) {
		t.Fatal("ordered restart lost saved session")
	}
	report := map[string]any{"checks": []string{"packaged worker runs as current nonroot UID", "backend requires private BasicAuth", "worker strips inherited secrets", "daemon state survives ordered worker restart"}, "uid": person.UID, "boundary": "single real UID; privileged broker, account switching and FN entitlement not exercised"}
	if output := os.Getenv("FNTUIOS_E2E_REPORT"); output != "" {
		data, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(output, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestNativeBrowser serves real packaged web through peer-verified Unix ingress.
func TestNativeBrowser(t *testing.T) {
	fixture := newNativeFixture(t)
	host := os.Getenv("FNTUIOS_E2E_BROWSER_HOST")
	if !strings.HasPrefix(host, "127.0.0.1:") {
		t.Fatal("native browser host must be loopback")
	}
	fixture.cfg.AllowedHosts = []string{host}
	if mode := remoteBrowserPolicy(); mode != "" {
		if mode != "strict" && mode != "repaired" {
			t.Fatal("invalid remote browser policy")
		}
		fixture.cfg.AllowedHosts = []string{"gateway.fixture"}
		allow := mode == "repaired"
		fixture.cfg.AllowSameOriginFetch = &allow
	}
	fixture.cfg.ExternalOriginPorts = map[string][]uint16{"http": {}, "https": {}}
	fixture.startWorker()
	defer fixture.stopWorker()
	socket := filepath.Join(fixture.root, "gateway.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	broker, gatewayConn := net.Pipe()
	defer broker.Close()
	defer gatewayConn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		decoder, encoder := json.NewDecoder(broker), json.NewEncoder(broker)
		for {
			var request identity.Request
			if decoder.Decode(&request) != nil {
				return
			}
			endpoint := fixture.endpoint
			if request.UID != fixture.users[0].UID || request.Name != fixture.users[0].Name {
				endpoint = gateway.Endpoint{UID: request.UID, Error: "native fixture has one principal"}
			}
			if encoder.Encode(endpoint) != nil {
				return
			}
		}
	}()
	go func() {
		if err := gateway.Serve(ctx, listener, gatewayConn, gateway.Options{Config: fixture.cfg, TrustedUIDs: []uint32{uint32(os.Geteuid())}}); err != nil {
			t.Log(err)
		}
	}()
	fixture.gatewaySocket = socket
	serveBrowserFixture(t, fixture.containerFixture, host, host)
}
