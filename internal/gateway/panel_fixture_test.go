package gateway

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fntuios/internal/config"
	"github.com/user-no-found/FnPackKit/runtime/backend"
)

// panelFixture runs the actual gateway against an inert broker, never a user process.
type panelFixture struct {
	client    *http.Client
	transport *http.Transport
	paused    atomic.Bool
	commands  atomic.Int32
	mutations atomic.Int32
	wrongUID  atomic.Bool
}

func newPanelFixture(t *testing.T, business http.Handler, authorities ...string) *panelFixture {
	t.Helper()
	if business == nil {
		business = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<!doctype html><html><head></head><body>fake terminal</body></html>"))
		})
	}
	upstream := httptest.NewServer(business)
	t.Cleanup(upstream.Close)
	fixture := &panelFixture{}
	ctx, cancel := context.WithCancel(context.Background())
	socket := filepath.Join(t.TempDir(), "panel.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	broker, remote := net.Pipe()
	brokerDone := make(chan struct{})
	go func() {
		defer close(brokerDone)
		decoder, encoder := json.NewDecoder(remote), json.NewEncoder(remote)
		for {
			var request BrokerRequest
			if decoder.Decode(&request) != nil {
				return
			}
			if request.UID != 1000 {
				fixture.wrongUID.Store(true)
			}
			endpoint := Endpoint{UID: request.UID, Paused: fixture.paused.Load(), Address: strings.TrimPrefix(upstream.URL, "http://"), Password: strings.Repeat("f", 43)}
			if request.Control != nil {
				fixture.commands.Add(1)
				if request.Control.Action != "status" {
					fixture.mutations.Add(1)
				}
				state := "running"
				if endpoint.Paused {
					state = "paused"
				}
				endpoint.Runtime = &backend.Snapshot{Schema: 1, State: state, Paused: endpoint.Paused}
			}
			if encoder.Encode(endpoint) != nil {
				return
			}
		}
	}()
	cfg := config.Config{Prefix: "/app/tuios", AllowedHosts: append([]string{"nas.example"}, authorities...),
		ConnectionLifetimeSeconds: 60, StartupTimeoutSeconds: 1, ShutdownTimeoutSeconds: 1}
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, listener, broker, Options{Name: "FnTuiOS", Config: cfg, TrustedUIDs: []uint32{uint32(os.Geteuid())}})
	}()
	fixture.transport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	fixture.client = &http.Client{Transport: fixture.transport, Timeout: 3 * time.Second}
	t.Cleanup(func() {
		fixture.transport.CloseIdleConnections()
		cancel()
		broker.Close()
		remote.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("panel gateway did not stop")
		}
		<-brokerDone
		if fixture.wrongUID.Load() || fixture.mutations.Load() != 0 {
			t.Error("panel fixture changed identity or requested a backend mutation")
		}
	})
	return fixture
}

func panelRequest(t *testing.T, method, path string) *http.Request {
	t.Helper()
	r, err := http.NewRequest(method, "http://nas.example/app/tuios/"+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("X-Trim-Userid", "1000")
	r.Header.Set("X-Trim-Username", "panel-user")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Origin", "http://nas.example")
	return r
}
