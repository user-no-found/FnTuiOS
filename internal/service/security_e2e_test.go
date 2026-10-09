package service

import (
	"fmt"
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

	"github.com/user-no-found/FnPackKit/runtime/identity"
)

func (fixture *containerFixture) exercise() {
	alice, bob := fixture.users[0], fixture.users[1]
	for _, attack := range []func(*http.Request){
		func(r *http.Request) { r.Header.Del("X-Trim-Userid") },
		func(r *http.Request) { r.Header.Add("X-Trim-Userid", "45001") },
		func(r *http.Request) { r.Header.Set("X-Trim-Userid", "0") },
		func(r *http.Request) { r.Header.Set("X-Trim-Userid", "999") },
		func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") },
		func(r *http.Request) { r.Header.Set("Origin", "http://nas.example:8080") },
		func(r *http.Request) { r.Header.Set("Origin", "https://nas.example:5666") },
		func(r *http.Request) { r.Host = "evil.example" },
	} {
		fixture.wantStatus(alice, "/app/tuios/", attack, 403)
	}
	fixture.wantStatus(alice, "/app/tuios/", func(r *http.Request) { r.Header.Set("X-Trim-Username", bob.Name) }, 503)
	fixture.wantStatus(alice, "/app/tuios/%2e%2e/ws", nil, 403)
	fixture.check("missing/duplicate/root/system/mismatched identities, hostile origin/host and path traversal rejected")
	for _, origin := range []string{"http://nas.example:5666", "https://nas.example:5667"} {
		fixture.wantStatus(alice, "/app/tuios", func(r *http.Request) { r.Header.Set("Origin", origin) }, http.StatusTemporaryRedirect)
	}
	fixture.check("configured NAS origin ports accepted after Host port stripping; other same-host ports rejected")
	for index, user := range fixture.users {
		page := fixture.wantStatus(user, "/app/tuios/", nil, 200)
		if !strings.Contains(page, "terminal.js") {
			fixture.t.Fatal("terminal page assets missing")
		}
		fixture.wantStatus(user, "/app/tuios/static/terminal.js", nil, 200)
		websocket := fixture.connectWebSocket(user)
		websocket.send(fixture.t, 2, []byte("0n"))
		fixture.waitCapture(user, "")
		command := fmt.Sprintf("i\x15printf 'FN_USER_%%s\\n' '%d'; printf 'FN_ID_%%s:%%s\\n' \"$(id -u)\" \"$HOME\"; printf 'FN_GROUPS_%%s\\n' \"$(id -G)\"; printf 'FN_SECRET_%%s\\n' \"${INHERITED_SECRET-unset}\"\r", index)
		websocket.send(fixture.t, 2, append([]byte{'0'}, []byte(command)...))
		capture := fixture.waitCapture(user, "FN_SECRET_unset")
		for _, expected := range []string{"FN_ID_" + strconv.FormatUint(uint64(user.UID), 10) + ":" + user.Home, "FN_SECRET_unset"} {
			if !strings.Contains(capture, expected) {
				fixture.t.Fatalf("missing %q: %s", expected, capture)
			}
		}
		if index == 0 && !strings.Contains(capture, "45002") {
			fixture.t.Fatal("supplementary NAS group missing")
		}
		websocket.send(fixture.t, 2, []byte(`{"uid":45001,"username":"fnbob"}`))
		websocket.send(fixture.t, 2, []byte("0printf 'FN_BOUND_%s\\n' \"$(id -u)\"\r"))
		fixture.waitCapture(user, "FN_BOUND_"+strconv.FormatUint(uint64(user.UID), 10))
		fixture.transcript += fmt.Sprintf("USER %s\n%s\n", user.Name, capture)
		websocket.connection.Close()
	}
	fixture.check("real WebSocket shell uses each NAS UID, HOME and supplementary groups; inherited secrets absent")
	fixture.check("WebSocket payload cannot change handshake-bound UID")
	for index, user := range fixture.users {
		capture := fixture.cli(user, "capture-pane", "-s", "web", "--scrollback")
		other := "FN_USER_" + strconv.Itoa(1-index)
		if strings.Contains(capture, other) {
			fixture.t.Fatalf("cross-user session marker %s leaked", other)
		}
	}
	fixture.check("same-named web sessions have independent per-user history")
	fixture.checkDirectAccess(alice, bob)
	fixture.checkPeer(alice)
}

func (fixture *containerFixture) waitCapture(user identity.User, marker string) string {
	fixture.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var capture string
	for time.Now().Before(deadline) {
		output, err := fixture.cliCommand(user, "capture-pane", "-s", "web", "--scrollback").CombinedOutput()
		capture = string(output)
		if err == nil && strings.Contains(capture, marker) {
			return capture
		}
		time.Sleep(100 * time.Millisecond)
	}
	fixture.t.Fatalf("shell marker %s missing: %s", marker, capture)
	return ""
}

func (fixture *containerFixture) checkDirectAccess(alice, bob identity.User) {
	// A user's private runtime blocks other local accounts even when they know its path.
	cmd := exec.Command("/bin/sh", "-c", "test ! -r \"$1\" && test ! -x \"$2\"", "sh", filepath.Join(fixture.cfg.StateDir, strconv.Itoa(int(alice.UID)), "runtime.log"), filepath.Join(fixture.cfg.RunDir, strconv.Itoa(int(alice.UID))))
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: bob.UID, Gid: bob.GID, Groups: bob.Groups}}
	if err := cmd.Run(); err != nil {
		fixture.t.Fatal("other NAS user can access private runtime or logs")
	}
	// Read only the container's own worker log to identify its loopback listener.
	data, err := os.ReadFile(filepath.Join(fixture.cfg.StateDir, strconv.Itoa(int(alice.UID)), "runtime.log"))
	if err != nil {
		fixture.t.Fatal(err)
	}
	var address string
	for _, field := range strings.Fields(string(data)) {
		if strings.HasPrefix(field, "http://127.0.0.1:") {
			address = strings.TrimRight(strings.TrimPrefix(field, "http://"), "/\"')")
		}
	}
	if address == "" {
		fixture.t.Fatalf("private listener address missing from own fixture log: %s", data)
	}
	response, err := http.Get("http://" + address + "/")
	if err != nil {
		fixture.t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		fixture.t.Fatalf("private web is unauthenticated: %d", response.StatusCode)
	}
	fixture.check("other local users cannot read runtime/logs and direct loopback web requires internal authentication")
}

func (fixture *containerFixture) checkPeer(user identity.User) {
	socket := filepath.Join(fixture.cfg.AppDir, "app.sock")
	// Deliberately widen only the disposable fixture socket so SO_PEERCRED, not mode, is exercised.
	if err := os.Chmod(socket, 0666); err != nil {
		fixture.t.Fatal(err)
	}
	defer os.Chmod(socket, 0600)
	executable, err := os.Executable()
	if err != nil {
		fixture.t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestUnauthorizedPeerHelper$")
	cmd.Env = []string{"FNTUIOS_UNTRUSTED_SOCKET=" + socket}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: user.UID, Gid: user.GID, Groups: user.Groups}}
	if output, err := cmd.CombinedOutput(); err != nil {
		fixture.t.Fatalf("peer check: %v %s", err, output)
	}
	fixture.check("SO_PEERCRED rejects a forged local peer even with a deliberately permissive fixture socket")
}

func TestUnauthorizedPeerHelper(t *testing.T) {
	socket := os.Getenv("FNTUIOS_UNTRUSTED_SOCKET")
	if socket == "" {
		t.Skip("peer subprocess only")
	}
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprint(conn, "GET /app/tuios/ HTTP/1.1\r\nHost: nas.example\r\nX-Trim-Userid: 45000\r\nX-Trim-Username: fnalice\r\n\r\n")
	data := make([]byte, 4096)
	count, err := conn.Read(data)
	if count > 0 || err == nil {
		t.Fatalf("untrusted peer received response: %q", data[:count])
	}
}
