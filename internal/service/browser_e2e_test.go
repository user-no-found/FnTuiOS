package service

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"
)

// TestContainerBrowser provides a disposable trusted-ingress fixture for Chromium.
// It is deliberately a test-only local entry and is never shipped as a service.
func TestContainerBrowser(t *testing.T) {
	if os.Getenv("FNTUIOS_E2E_BROWSER") != "1" {
		t.Skip("requires explicit browser fixture")
	}
	if os.Geteuid() != 0 {
		t.Fatal("browser fixture needs container root")
	}
	if _, err := os.Stat("/.dockerenv"); err != nil {
		t.Fatal("refusing account fixtures outside Docker")
	}
	host := os.Getenv("FNTUIOS_E2E_BROWSER_HOST")
	if host == "" {
		t.Fatal("FNTUIOS_E2E_BROWSER_HOST must be the mapped loopback Host")
	}
	root, err := os.MkdirTemp("/tmp", "fntuios-browser-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	if err := os.Chmod(root, 0711); err != nil {
		t.Fatal(err)
	}
	fixture := &containerFixture{t: t, root: root}
	fixture.prepare(os.Getenv("FNTUIOS_E2E_BIN_DIR"))
	fixture.cfg.AllowedHosts = []string{host}
	fixture.cfg.ConnectionLifetimeSeconds = 600
	data, _ := json.Marshal(fixture.cfg)
	if err := os.WriteFile(filepath.Join(root, "gateway.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	fixture.start()
	defer fixture.stop()
	serveBrowserFixture(t, fixture, host, ":8080")
}

func serveBrowserFixture(t *testing.T, fixture *containerFixture, host, bind string) {
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		socket := fixture.gatewaySocket
		if socket == "" {
			socket = filepath.Join(fixture.cfg.AppDir, "app.sock")
		}
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	evidence := &gatewayBrowserEvidence{}
	attacker := attackerBrowserFixture()
	defer attacker.Close()
	proxy := &httputil.ReverseProxy{Transport: transport, ModifyResponse: evidence.observe, Rewrite: func(request *httputil.ProxyRequest) {
		request.Out.URL.Scheme = "http"
		request.Out.URL.Host = "gateway.fixture"
		request.Out.Host = request.In.Host
		if remoteBrowserPolicy() != "" {
			request.Out.Host = "gateway.fixture"
		}
		user := fixture.users[0]
		if request.In.Header.Get("X-Fixture-User") == "bob" {
			user = fixture.users[1]
		}
		request.Out.Header.Del("X-Fixture-User")
		request.Out.Header.Set("X-Trim-Userid", strconv.Itoa(int(user.UID)))
		request.Out.Header.Set("X-Trim-Username", user.Name)
	}}
	stop := make(chan struct{}, 1)
	server := &http.Server{Addr: bind, ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host {
			http.Error(w, "fixture host rejected", 403)
			return
		}
		if evidence.serve(w, r, attacker) {
			return
		}
		if r.URL.Path == "/__fixture_identity" {
			json.NewEncoder(w).Encode(map[string]any{"uid": fixture.users[0].UID, "username": fixture.users[0].Name, "single_user": fixture.native})
			return
		}
		if fixture.native && r.Header.Get("X-Fixture-User") != "" {
			http.Error(w, "native fixture has one principal", 403)
			return
		}
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(`<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" href="data:,"><title>FnTuiOS gateway fixture</title><style>html,body,iframe{width:100%;height:100%;margin:0;border:0;overflow:hidden}</style></head><body><iframe title="FnTuiOS" src="/app/tuios/"></iframe></body></html>`))
			return
		}
		if r.URL.Path == "/__fixture_stop" && r.Method == "POST" {
			w.WriteHeader(204)
			select {
			case stop <- struct{}{}:
			default:
			}
			return
		}
		if r.URL.Path == "/__fixture_option" {
			if r.Method == "POST" {
				value := r.URL.Query().Get("value")
				if !slices.Contains([]string{"top", "bottom", "hidden"}, value) {
					http.Error(w, "invalid fixture option", 400)
					return
				}
				fixture.cli(fixture.users[0], "set-config", "appearance.dockbar_position", value, "-s", "web")
			} else if r.Method != "GET" {
				http.Error(w, "method rejected", 405)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(fixture.cli(fixture.users[0], "get-config", "appearance.dockbar_position", "--json", "-s", "web")))
			return
		}
		if fixture.browserImageFixture(w, r) {
			return
		}
		if fixture.browserImageAuthorityFixture(w, r) {
			return
		}
		proxy.ServeHTTP(w, r)
	})}
	defer server.Close()
	if remoteBrowserPolicy() != "" {
		listener, err := net.Listen("tcp", bind)
		if err != nil {
			t.Fatal(err)
		}
		tlsServer := httptest.NewUnstartedServer(server.Handler)
		tlsServer.Listener.Close()
		tlsServer.Listener = listener
		tlsServer.StartTLS()
		defer tlsServer.Close()
	} else {
		go func() {
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				t.Logf("fixture bridge: %v", err)
				select {
				case stop <- struct{}{}:
				default:
				}
			}
		}()
	}

	t.Log("browser fixture ready on", bind, "; stop with POST /__fixture_stop")
	select {
	case <-stop:
	case <-time.After(10 * time.Minute):
		t.Fatal("browser fixture timed out")
	}
}
