// Package gateway authenticates local fnOS peers and proxies each NAS user's service.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"fntuios/internal/config"
	"github.com/user-no-found/FnPackKit/runtime/backend"
	"github.com/user-no-found/FnPackKit/runtime/gatewaypolicy"
	"github.com/user-no-found/FnPackKit/runtime/identity"
	"github.com/user-no-found/FnPackKit/runtime/peer"
	"github.com/user-no-found/FnPackKit/runtime/runtimepanel"
)

// Endpoint is private broker data. Authentication never reaches the browser.
type Endpoint struct {
	UID      uint32            `json:"uid"`
	Address  string            `json:"address,omitempty"`
	Password string            `json:"password,omitempty"`
	Error    string            `json:"error,omitempty"`
	Paused   bool              `json:"paused,omitempty"`
	Runtime  *backend.Snapshot `json:"runtime,omitempty"`
}

// BrokerRequest carries only a trusted identity and a fixed lifecycle intent.
type BrokerRequest struct {
	identity.Request
	Control *backend.Command `json:"control,omitempty"`
}

// Options is passed through an inherited descriptor to the unprivileged gateway.
type Options struct {
	Name        string
	Config      config.Config
	TrustedUIDs []uint32
}

type brokerClient struct {
	mu         sync.Mutex
	connection net.Conn
	decoder    *json.Decoder
	encoder    *json.Encoder
	timeout    time.Duration
	failed     bool
}

func (client *brokerClient) exchange(request BrokerRequest) (Endpoint, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.failed {
		return Endpoint{}, errors.New("control channel is closed")
	}
	fail := func(err error) (Endpoint, error) {
		client.failed = true
		client.connection.Close()
		return Endpoint{}, err
	}
	if err := client.connection.SetDeadline(time.Now().Add(client.timeout)); err != nil {
		return fail(err)
	}
	if err := client.encoder.Encode(request); err != nil {
		return fail(err)
	}
	var endpoint Endpoint
	if err := client.decoder.Decode(&endpoint); err != nil {
		return fail(err)
	}
	if endpoint.UID != request.UID {
		return fail(errors.New("broker response identity mismatch"))
	}
	return endpoint, nil
}

func (client *brokerClient) endpoint(request identity.Request) (Endpoint, error) {
	endpoint, err := client.exchange(BrokerRequest{Request: request})
	if err != nil {
		return Endpoint{}, err
	}
	if endpoint.Paused {
		return Endpoint{}, backend.ErrPaused
	}
	if endpoint.Error != "" {
		return Endpoint{}, errors.New(endpoint.Error)
	}
	host, port, err := net.SplitHostPort(endpoint.Address)
	if err != nil || host != "127.0.0.1" || port == "" || len(endpoint.Password) < 32 {
		client.mu.Lock()
		client.failed = true
		client.connection.Close()
		client.mu.Unlock()
		return Endpoint{}, errors.New("invalid private endpoint")
	}
	return endpoint, nil
}

func (client *brokerClient) control(ctx context.Context, request identity.Request, command backend.Command) (backend.Snapshot, error) {
	endpoint, err := client.exchange(BrokerRequest{Request: request, Control: &command})
	if err != nil {
		return backend.Snapshot{}, err
	}
	if endpoint.Runtime == nil {
		return backend.Snapshot{}, errors.New("runtime status missing")
	}
	if endpoint.Error != "" {
		return *endpoint.Runtime, errors.New(endpoint.Error)
	}
	return *endpoint.Runtime, nil
}

// Serve accepts only configured OS peers, then binds each request to a NAS UID.
func Serve(ctx context.Context, listener net.Listener, broker net.Conn, options Options) error {
	policy, err := gatewaypolicy.New(options.Config.GatewayPolicy())
	if err != nil {
		return err
	}
	client := &brokerClient{connection: broker, decoder: json.NewDecoder(broker), encoder: json.NewEncoder(broker), timeout: time.Duration(options.Config.ShutdownTimeoutSeconds+options.Config.StartupTimeoutSeconds+15) * time.Second}
	trusted, err := peer.NewListener(listener, options.TrustedUIDs, time.Duration(options.Config.ConnectionLifetimeSeconds)*time.Second)
	if err != nil {
		return err
	}
	transport := &http.Transport{Proxy: nil, MaxIdleConns: 64, MaxIdleConnsPerHost: 8, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: time.Duration(options.Config.StartupTimeoutSeconds) * time.Second}
	defer transport.CloseIdleConnections()
	controls := runtimepanel.Handler{Prefix: options.Config.Prefix, Name: options.Name, Execute: client.control, Actions: []runtimepanel.Action{
		{ID: "split-up", Label: "上分屏", Selector: "#fn-panel-split-up", Group: "split", Columns: 2, Audience: "pointer"},
		{ID: "split-down", Label: "下分屏", Selector: "#fn-panel-split-down", Group: "split", Columns: 2, Audience: "pointer"},
		{ID: "split-left", Label: "左分屏", Selector: "#fn-panel-split-left", Group: "split", Columns: 2, Audience: "pointer"},
		{ID: "split-right", Label: "右分屏", Selector: "#fn-panel-split-right", Group: "split", Columns: 2, Audience: "pointer"},
		{ID: "terminal-settings", Label: "终端显示", Selector: "#settings-toggle", Group: "terminal", Columns: 2},
		{ID: "copy-text", Label: "复制文本", Selector: "#fn-panel-copy-text", Group: "terminal", Columns: 2},
		{ID: "ctrl", Label: "Ctrl", Selector: "#fn-panel-ctrl", Group: "modifiers", Columns: 3, Audience: "touch", KeepOpen: true},
		{ID: "shift", Label: "Shift", Selector: "#fn-panel-shift", Group: "modifiers", Columns: 3, Audience: "touch", KeepOpen: true},
		{ID: "alt", Label: "Alt", Selector: "#fn-panel-alt", Group: "modifiers", Columns: 3, Audience: "touch", KeepOpen: true},
		{ID: "key-up", Label: "↑", Selector: "#fn-panel-key-up", Group: "arrows", Columns: 2, Audience: "touch", KeepOpen: true},
		{ID: "key-down", Label: "↓", Selector: "#fn-panel-key-down", Group: "arrows", Columns: 2, Audience: "touch", KeepOpen: true},
		{ID: "key-left", Label: "←", Selector: "#fn-panel-key-left", Group: "arrows", Columns: 2, Audience: "touch", KeepOpen: true},
		{ID: "key-right", Label: "→", Selector: "#fn-panel-key-right", Group: "arrows", Columns: 2, Audience: "touch", KeepOpen: true},
		{ID: "photo", Label: "添加照片", Selector: "#fn-panel-photo", Group: "phone-input", Columns: 2, Audience: "touch"},
		{ID: "keyboard", Label: "键盘", Selector: "#fn-panel-keyboard", Group: "phone-input", Columns: 2, Audience: "touch"},
	}}
	controls.Layout = &runtimepanel.Layout{Avoid: "#sip-keybar"}
	panelFiles, err := newPanelAssets(&controls)
	if err != nil {
		return err
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request, rejection := policy.Check(r)
		if rejection == nil {
			if _, err := applicationCookie(r); err != nil {
				rejection = &gatewaypolicy.Rejection{Code: "invalid_cookie"}
			}
		}
		if rejection != nil {
			writeFailure(w, r, rejection, http.StatusForbidden)
			return
		}
		if panelFiles.serve(w, r) || controls.Handle(w, r, request) {
			return
		}
		if r.URL.Path == options.Config.Prefix+"/_gateway/grant" {
			serveGatewayGrant(w, r, policy, options.Config.Prefix+"/ws")
			return
		}
		if r.URL.Path == options.Config.Prefix+"/_gateway/status" {
			serveGatewayStatus(w, r)
			return
		}
		if r.URL.Path == options.Config.Prefix {
			http.Redirect(w, r, options.Config.Prefix+"/", http.StatusTemporaryRedirect)
			return
		}
		endpoint, err := client.endpoint(request)
		if errors.Is(err, backend.ErrPaused) {
			controls.Unavailable(w, r)
			return
		}
		if err != nil {
			writeFailure(w, r, &gatewaypolicy.Rejection{Code: "runtime_unavailable"}, http.StatusServiceUnavailable)
			return
		}
		proxy := newProxy(endpoint, options.Config.Prefix, transport)
		proxy.ServeHTTP(w, r)
	})
	server := &http.Server{Handler: handler, ConnContext: trusted.ConnContext, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	go func() { <-ctx.Done(); trusted.CloseConnections(); server.Close() }()
	err = server.Serve(trusted)
	if errors.Is(err, http.ErrServerClosed) || ctx.Err() != nil {
		return nil
	}
	return err
}

func newProxy(endpoint Endpoint, prefix string, transport http.RoundTripper) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(request *httputil.ProxyRequest) {
			request.Out.URL.Scheme = "http"
			request.Out.URL.Host = endpoint.Address
			request.Out.URL.Path = strings.TrimPrefix(request.In.URL.Path, prefix)
			request.Out.URL.RawPath = ""
			request.Out.Host = endpoint.Address
			for key := range request.Out.Header {
				lower := strings.ToLower(key)
				if strings.HasPrefix(lower, "x-trim-") || strings.HasPrefix(lower, "x-forwarded-") || lower == "forwarded" || lower == "authorization" || lower == "cookie" || lower == "proxy-authorization" {
					request.Out.Header.Del(key)
				}
			}
			request.Out.Header.Set("Accept-Encoding", "identity")
			request.Out.SetBasicAuth("fntuios", endpoint.Password)
			if cookie, err := applicationCookie(request.In); err == nil && cookie != nil {
				request.Out.AddCookie(cookie)
			}
			if request.In.Header.Get("Origin") != "" {
				request.Out.Header.Set("Origin", "http://"+endpoint.Address)
			}
		},
		ModifyResponse: func(response *http.Response) error {
			if err := rewriteResponse(response, prefix, endpoint.Address); err != nil {
				return err
			}
			if err := runtimepanel.Inject(response, prefix); err != nil {
				return err
			}
			return gatewaypolicy.CompleteHandshake(response)
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, _ error) {
			writeFailure(w, r, &gatewaypolicy.Rejection{Code: "proxy_failure"}, http.StatusBadGateway)
		},
		ErrorLog: log.New(io.Discard, "", 0),
	}
}

func rewriteResponse(response *http.Response, prefix, address string) error {
	response.Header.Del("WWW-Authenticate")
	response.Header.Set("Cache-Control", "no-store")
	response.Header.Set("X-Content-Type-Options", "nosniff")
	response.Header.Set("X-Frame-Options", "SAMEORIGIN")
	response.Header.Set("Content-Security-Policy", "frame-ancestors 'self'; object-src 'none'; base-uri 'self'")
	if location := response.Header.Get("Location"); location != "" {
		target, err := url.Parse(location)
		if err != nil || target.User != nil || target.Host != "" && target.Host != address {
			return fmt.Errorf("backend returned an unsafe redirect")
		}
		if target.Host != "" || strings.HasPrefix(target.Path, "/") {
			target.Scheme = ""
			target.Host = ""
			target.Path = prefix + "/" + strings.TrimPrefix(target.Path, "/")
			response.Header.Set("Location", target.String())
		}
	}
	cookies := response.Cookies()
	response.Header.Del("Set-Cookie")
	for _, cookie := range cookies {
		if cookie.Name != "tuios_inbox" || !validInboxID(cookie.Value) {
			continue
		}
		cookie.Name = "fntuios_inbox"
		cookie.Domain = ""
		cookie.Path = prefix + "/" + strings.TrimPrefix(cookie.Path, "/")
		cookie.SameSite = http.SameSiteStrictMode
		response.Header.Add("Set-Cookie", cookie.String())
	}
	return nil
}
