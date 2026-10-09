package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
)

// gatewayBrowserEvidence records protocol outcomes in the isolated browser
// fixture, without logging request addresses, credentials, or terminal bytes.
type gatewayBrowserEvidence struct {
	mu     sync.Mutex
	events []gatewayBrowserEvent
}

type gatewayBrowserEvent struct {
	Route     string `json:"route"`
	Status    int    `json:"status"`
	FetchSite string `json:"fetch_site"`
	Origin    bool   `json:"has_origin"`
}

func remoteBrowserPolicy() string { return os.Getenv("FNTUIOS_E2E_REMOTE_POLICY") }

func (evidence *gatewayBrowserEvidence) observe(response *http.Response) error {
	request := response.Request
	route := "resource"
	switch {
	case strings.HasSuffix(request.URL.Path, "/ws"):
		route = "websocket"
	case strings.Contains(request.URL.Path, "/fonts/"):
		route = "font"
	case strings.HasSuffix(request.URL.Path, "/_gateway/grant"):
		route = "grant"
	case strings.HasSuffix(request.URL.Path, "/_gateway/status"):
		route = "diagnostic"
	}
	evidence.mu.Lock()
	evidence.events = append(evidence.events, gatewayBrowserEvent{route, response.StatusCode,
		request.Header.Get("Sec-Fetch-Site"), request.Header.Get("Origin") != ""})
	evidence.mu.Unlock()
	return nil
}

func (evidence *gatewayBrowserEvidence) serve(w http.ResponseWriter, r *http.Request, attacker *httptest.Server) bool {
	if r.URL.Path != "/__fixture_gateway_events" {
		return false
	}
	evidence.mu.Lock()
	defer evidence.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]any{"events": evidence.events, "attacker": attacker.URL})
	return true
}

func attackerBrowserFixture() *httptest.Server {
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><title>Different-port browser fixture</title>"))
	}))
}
