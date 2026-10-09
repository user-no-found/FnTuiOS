package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"fntuios/internal/config"
)

// Health verifies that the local gateway is serving and rejects anonymous requests.
func Health(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(cfg.AppDir, "app.sock"))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get("http://localhost" + cfg.Prefix + "/")
	if err != nil {
		return err
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden && response.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("gateway health returned unexpected status %d", response.StatusCode)
	}
	return nil
}
