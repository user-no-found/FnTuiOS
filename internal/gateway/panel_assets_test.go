package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/user-no-found/FnPackKit/runtime/identity"
	"github.com/user-no-found/FnPackKit/runtime/runtimepanel"
)

func TestPanelAssetsAreSharedBytes(t *testing.T) {
	fixture := newPanelFixture(t, nil)
	shared := runtimepanel.Handler{Prefix: "/app/tuios"}
	for _, name := range []string{"bootstrap.js", "panel.js", "scope.js", "source.js", "extensions.js", "drag.js", "presentation.js", "viewport.js", "state.js", "geometry.js", "panel.css"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(method+"/"+name, func(t *testing.T) {
				r := panelRequest(t, method, "_runtime/"+name)
				expected := httptest.NewRecorder()
				if !shared.Handle(expected, r, identity.Request{UID: 1000}) || expected.Code != 200 {
					t.Fatal("shared resource unavailable")
				}
				response, err := fixture.client.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				data, err := io.ReadAll(response.Body)
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != 200 || !bytes.Equal(data, expected.Body.Bytes()) || response.Header.Get("Content-Type") != expected.Header().Get("Content-Type") {
					t.Fatal("gateway modified shared bytes or media type")
				}
				if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Content-Type-Options") != "nosniff" {
					t.Fatal("shared response headers missing")
				}
			})
		}
	}
	if fixture.commands.Load() != 0 {
		t.Fatal("loading resources contacted runtime controls")
	}
}

func TestPanelResourcesRetainGatewayBoundary(t *testing.T) {
	fixture := newPanelFixture(t, nil)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, name := range []string{"panel.js", "state.js", "geometry.js", "manifest"} {
			for _, tc := range []struct {
				name   string
				mutate func(*http.Request)
			}{
				{"no identity", func(r *http.Request) { r.Header.Del("X-Trim-Userid") }},
				{"bad identity", func(r *http.Request) { r.Header.Set("X-Trim-Userid", "invalid") }},
				{"cross origin", func(r *http.Request) {
					r.Header.Set("Origin", "https://foreign.example")
					r.Header.Set("Sec-Fetch-Site", "cross-site")
				}},
				{"bad cookie", func(r *http.Request) { r.Header.Set("Cookie", "fntuios_inbox=1; fntuios_inbox=2") }},
			} {
				t.Run(method+"/"+name+"/"+tc.name, func(t *testing.T) {
					r := panelRequest(t, method, "_runtime/"+name)
					tc.mutate(r)
					response, err := fixture.client.Do(r)
					if err != nil {
						t.Fatal(err)
					}
					response.Body.Close()
					if response.StatusCode != 403 {
						t.Fatalf("status %d, want 403", response.StatusCode)
					}
				})
			}
		}
	}
	for _, tc := range []struct {
		method, path string
		code         int
	}{
		{"POST", "state.js", 405}, {"POST", "geometry.js", 405},
		{"GET", "fntuios-idle.js", 404}, {"HEAD", "fntuios-idle.js", 404}, {"GET", "controls.css", 404},
	} {
		response, err := fixture.client.Do(panelRequest(t, tc.method, "_runtime/"+tc.path))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != tc.code || tc.code == 405 && response.Header.Get("Allow") != "GET, HEAD" {
			t.Fatalf("%s %s status %d", tc.method, tc.path, response.StatusCode)
		}
	}
	if fixture.commands.Load() != 0 {
		t.Fatal("rejected resources reached runtime")
	}
}

func TestPanelBusinessManifestAndPausedGate(t *testing.T) {
	fixture := newPanelFixture(t, nil)
	response, err := fixture.client.Do(panelRequest(t, "GET", "_runtime/manifest"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Actions []runtimepanel.Action `json:"actions"`
		Layout  runtimepanel.Layout   `json:"layout"`
	}
	err = json.NewDecoder(response.Body).Decode(&manifest)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"split-up", "split-down", "split-left", "split-right", "terminal-settings", "copy-text", "ctrl", "shift", "alt", "key-up", "key-down", "key-left", "key-right", "photo", "keyboard"}
	var actual []string
	for _, action := range manifest.Actions {
		actual = append(actual, action.ID)
		keep := action.Group == "modifiers" || action.Group == "arrows"
		if action.Selector == "" || action.KeepOpen != keep {
			t.Fatalf("business action changed: %s", action.ID)
		}
	}
	if !reflect.DeepEqual(actual, ids) || manifest.Layout.Avoid != "#sip-keybar" {
		t.Fatal("business actions or keyboard avoidance changed")
	}
	fixture.paused.Store(true)
	for _, tc := range []struct {
		path string
		code int
		text string
	}{
		{"", 200, "data-runtime-page"}, {"static/terminal.js", 503, "backend_paused"},
		{"_runtime/state.js", 200, ""}, {"_runtime/control", 200, `"state":"paused"`},
	} {
		response, err := fixture.client.Do(panelRequest(t, "GET", tc.path))
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != tc.code || !bytes.Contains(data, []byte(tc.text)) {
			t.Fatalf("paused %s status %d: %s", tc.path, response.StatusCode, data)
		}
	}
}
