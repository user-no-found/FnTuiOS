package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/user-no-found/FnPackKit/runtime/identity"
)

// TestContainerRuntimePanel exercises the shipped broker and real workers under two synthetic NSS accounts.
func TestContainerRuntimePanel(t *testing.T) {
	if os.Getenv("FNTUIOS_E2E_CONTAINER") != "1" {
		t.Skip("requires explicitly isolated container")
	}
	if os.Geteuid() != 0 {
		t.Fatal("container root is required")
	}
	if _, err := os.Stat("/.dockerenv"); err != nil {
		t.Fatal("refusing account fixtures outside Docker")
	}
	root, err := os.MkdirTemp("/tmp", "runtime-panel-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	if err = os.Chmod(root, 0711); err != nil {
		t.Fatal(err)
	}
	f := &containerFixture{t: t, root: root}
	f.prepare(os.Getenv("FNTUIOS_E2E_BIN_DIR"))
	// Omitted optional settings exercise the application's real centralized defaults.
	configFile := filepath.Join(root, "gateway.json")
	raw, _ := os.ReadFile(configFile)
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"allow_same_origin_fetch", "gateway_grant_lifetime_seconds", "gateway_grant_capacity", "gateway_grant_per_user", "state_restore_max_entries", "state_restore_max_depth"} {
		delete(settings, key)
	}
	raw, _ = json.Marshal(settings)
	if err := os.WriteFile(configFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	defer f.stop()
	f.start()
	call := func(user identity.User, method, suffix, body string, headers map[string]string) (int, []byte) {
		r, _ := http.NewRequest(method, "http://nas.example"+f.cfg.Prefix+suffix, strings.NewReader(body))
		r.Header.Set("X-Trim-Userid", strconv.Itoa(int(user.UID)))
		r.Header.Set("X-Trim-Username", user.Name)
		r.Header.Set("Origin", "http://nas.example")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-FnPackKit-Control", "1")
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		res, err := f.client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		return res.StatusCode, data
	}
	state := func(user identity.User, want string) {
		code, data := call(user, "GET", "/_runtime/control", "", nil)
		var result struct{ State string }
		_ = json.Unmarshal(data, &result)
		if code != 200 || result.State != want {
			t.Fatalf("uid %d: %d %s expected %s", user.UID, code, data, want)
		}
	}
	action := func(user identity.User, body string) {
		code, data := call(user, "POST", "/_runtime/control", body, nil)
		if code != 200 {
			t.Fatalf("action %d %s", code, data)
		}
	}
	alice, bob := f.users[0], f.users[1]
	state(alice, "stopped")
	state(bob, "stopped")
	f.wantStatus(alice, f.cfg.Prefix+"/", nil, 200)
	f.wantStatus(bob, f.cfg.Prefix+"/", nil, 200)
	state(alice, "running")
	state(bob, "running")
	f.cli(alice, "new", "web", "--detach")
	f.cli(alice, "new-window", "panel-task", "-s", "web", "--", "/bin/bash", "-c", "echo $$ > "+filepath.Join(alice.Home, "panel.pid")+"; echo PANEL_HISTORY; sleep 300")
	pidFile := filepath.Join(alice.Home, "panel.pid")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(pidFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("terminal task not created")
		}
		time.Sleep(50 * time.Millisecond)
	}
	data, _ := os.ReadFile(pidFile)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	if pid < 2 {
		t.Fatal("missing terminal process identity")
	}
	code, _ := call(alice, "POST", "/_runtime/control", `{"action":"stop"}`, nil)
	if code != 400 {
		t.Fatal("unconfirmed shutdown accepted", code)
	}
	state(alice, "running")
	code, _ = call(alice, "POST", "/_runtime/control", `{"action":"stop","confirm":true}`, map[string]string{"Origin": "http://evil.example", "Sec-Fetch-Site": "cross-site"})
	if code != 403 {
		t.Fatal("cross-site shutdown accepted", code)
	}
	action(alice, `{"action":"stop","confirm":true}`)
	state(alice, "paused")
	state(bob, "running")
	for i := 0; i < 3; i++ {
		code, body := call(alice, "GET", "/", "", nil)
		if code != 200 || !strings.Contains(string(body), "data-runtime-page") {
			t.Fatal("paused navigation unavailable", code, string(body))
		}
		state(alice, "paused")
	}
	if _, err := os.Stat(filepath.Join(f.cfg.RunDir, strconv.Itoa(int(alice.UID)), "tuios/tuios.sock")); !os.IsNotExist(err) {
		t.Fatal("paused daemon remains active", err)
	}
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		tail := strings.LastIndex(string(data), ") ")
		if tail < 0 || !strings.HasPrefix(string(data)[tail+2:], "Z ") {
			t.Fatal("terminal process still executing after confirmed stop")
		}
	}
	action(alice, `{"action":"start"}`)
	state(alice, "running")
	if !strings.Contains(f.cli(alice, "capture-pane", "-s", "web", "--scrollback"), "PANEL_HISTORY") {
		t.Fatal("stop/start lost terminal history")
	}
	action(alice, `{"action":"restart","confirm":true}`)
	state(alice, "running")
	state(bob, "running")
	t.Log("verified root broker, real NSS isolation, manual pause, stopped navigation, task termination, session history, restart, and cross-site rejection")
}
