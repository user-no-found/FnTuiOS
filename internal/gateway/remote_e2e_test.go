package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fntuios/internal/config"
)

// TestGatewayDiagnosticIngress uses an authenticated Unix peer and the actual
// HTTP handler. The broker deliberately has no responder: a diagnostic must not
// launch a runtime or touch a terminal session.
func TestGatewayDiagnosticIngress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	socket := filepath.Join(t.TempDir(), "gateway.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	broker, remote := net.Pipe()
	defer broker.Close()
	defer remote.Close()
	cfg := config.Config{Prefix: "/app/tuios", AllowedHosts: []string{"nas.example"},
		ExternalOriginPorts:       map[string][]uint16{"https": {5667}},
		ConnectionLifetimeSeconds: 60, StartupTimeoutSeconds: 1, ShutdownTimeoutSeconds: 1}
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, listener, broker, Options{Config: cfg, TrustedUIDs: []uint32{uint32(os.Geteuid())}})
	}()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("gateway did not stop")
		}
	}()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	for _, tc := range []struct {
		name, origin, site string
		mutate             func(*http.Request)
		status             int
	}{
		{"public authority restored", "https://public.example:9443", "same-origin", nil, 200},
		{"explicit old NAS port", "https://nas.example:5667", "", nil, 200},
		{"metadata absent stays strict", "https://public.example:9443", "", nil, 403},
		{"same site is not same origin", "https://nas.example:9443", "same-site", nil, 403},
		{"cross site rejected", "https://public.example:9443", "cross-site", nil, 403},
		{"malformed origin rejected", "null", "same-origin", nil, 403},
		{"missing identity rejected", "https://public.example:9443", "same-origin", func(r *http.Request) { r.Header.Del("X-Trim-Userid") }, 403},
		{"cookie boundary retained", "https://public.example:9443", "same-origin", func(r *http.Request) {
			r.Header.Set("Cookie", "fntuios_inbox=1; fntuios_inbox=2; NAS_LOGIN=PRIVATE_SENTINEL")
		}, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := http.NewRequest("POST", "http://nas.example/app/tuios/_gateway/status", nil)
			if err != nil {
				t.Fatal(err)
			}
			r.Header.Set("Origin", tc.origin)
			if tc.site != "" {
				r.Header.Set("Sec-Fetch-Site", tc.site)
			}
			r.Header.Set("X-Trim-Userid", "1000")
			r.Header.Set("X-Trim-Username", "PRIVATE_SENTINEL")
			r.Header.Set("X-Request-Id", "PRIVATE_SENTINEL")
			if tc.mutate != nil {
				tc.mutate(r)
			}
			response, err := client.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != tc.status {
				t.Fatalf("status %d, want %d", response.StatusCode, tc.status)
			}
			if strings.Contains(string(body), "PRIVATE_SENTINEL") || strings.Contains(string(body), "public.example") {
				t.Fatal("diagnostic reflected private request data")
			}
			var result struct {
				Schema    int    `json:"schema"`
				Status    string `json:"status"`
				Code      string `json:"code"`
				RequestID string `json:"request_id"`
			}
			if err := json.Unmarshal(body, &result); err != nil {
				t.Fatal(err)
			}
			if result.Schema != 1 || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatal("diagnostic schema or cache boundary missing")
			}
			if tc.status == 200 && result.Status != "ready" {
				t.Fatal("authorized HTTP diagnostic not ready")
			}
			if tc.status == 403 && (result.Status != "rejected" || result.Code == "" || result.RequestID == "") {
				t.Fatal("rejection lacks safe classification and server correlation")
			}
		})
	}
}
