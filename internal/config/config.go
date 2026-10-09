// Package config owns the editable deployment configuration for FnTuiOS.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/user-no-found/FnPackKit/runtime/gatewaypolicy"
)

const (
	defaultGatewayGrantLifetimeSeconds = 30
	defaultGatewayGrantCapacity        = 512
	defaultGatewayGrantPerUser         = 8
	defaultStateRestoreMaxEntries      = 100000
	defaultStateRestoreMaxDepth        = 64
)

// Config is loaded once before the privileged supervisor starts any children.
type Config struct {
	AppDir                      string              `json:"app_dir"`
	StateDir                    string              `json:"state_dir"`
	RunDir                      string              `json:"run_dir"`
	Prefix                      string              `json:"prefix"`
	GatewayAccount              string              `json:"gateway_account"`
	TrustedGatewayAccounts      []string            `json:"trusted_gateway_accounts"`
	AllowedHosts                []string            `json:"allowed_hosts"`
	ExternalOriginPorts         map[string][]uint16 `json:"external_origin_ports"`
	AllowedOrigins              []string            `json:"allowed_origins"`
	AllowSameOriginFetch        *bool               `json:"allow_same_origin_fetch"`
	GatewayGrantLifetimeSeconds int                 `json:"gateway_grant_lifetime_seconds"`
	GatewayGrantCapacity        int                 `json:"gateway_grant_capacity"`
	GatewayGrantPerUser         int                 `json:"gateway_grant_per_user"`
	MaxUsers                    int                 `json:"max_users"`
	MaxConnectionsPerUser       int                 `json:"max_connections_per_user"`
	ConnectionLifetimeSeconds   int                 `json:"connection_lifetime_seconds"`
	StartupTimeoutSeconds       int                 `json:"startup_timeout_seconds"`
	ShutdownTimeoutSeconds      int                 `json:"shutdown_timeout_seconds"`
	StateRestoreMaxEntries      int                 `json:"state_restore_max_entries"`
	StateRestoreMaxDepth        int                 `json:"state_restore_max_depth"`
	UserPath                    string              `json:"user_path"`
	Language                    string              `json:"language"`
	TerminalShell               string              `json:"terminal_shell"`
}

// Load refuses unknown keys so deployment typos cannot weaken an intended policy.
func Load(path string) (Config, error) {
	enabled := true
	cfg := Config{AllowSameOriginFetch: &enabled, GatewayGrantLifetimeSeconds: defaultGatewayGrantLifetimeSeconds, GatewayGrantCapacity: defaultGatewayGrantCapacity, GatewayGrantPerUser: defaultGatewayGrantPerUser}
	cfg.StateRestoreMaxEntries = defaultStateRestoreMaxEntries
	cfg.StateRestoreMaxDepth = defaultStateRestoreMaxDepth
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if len(data) > 65536 {
		return cfg, errors.New("configuration exceeds 64 KiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	var restoreFields struct {
		Entries json.RawMessage `json:"state_restore_max_entries"`
		Depth   json.RawMessage `json:"state_restore_max_depth"`
	}
	if err := json.Unmarshal(data, &restoreFields); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	for key, value := range map[string]json.RawMessage{"state_restore_max_entries": restoreFields.Entries, "state_restore_max_depth": restoreFields.Depth} {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return cfg, fmt.Errorf("%s must be an integer", key)
		}
	}
	if cfg.AllowSameOriginFetch == nil {
		return cfg, errors.New("allow_same_origin_fetch must be a boolean")
	}
	if cfg.GatewayGrantLifetimeSeconds < 1 || cfg.GatewayGrantLifetimeSeconds > 300 || cfg.GatewayGrantCapacity < 1 || cfg.GatewayGrantCapacity > 8192 || cfg.GatewayGrantPerUser < 1 || cfg.GatewayGrantPerUser > cfg.GatewayGrantCapacity {
		return cfg, errors.New("gateway_grant_lifetime_seconds must be 1..300 and gateway_grant_capacity 1..8192 and gateway_grant_per_user 1..capacity")
	}
	if cfg.StateRestoreMaxEntries < 1 || cfg.StateRestoreMaxEntries > 1000000 || cfg.StateRestoreMaxDepth < 1 || cfg.StateRestoreMaxDepth > 256 {
		return cfg, errors.New("state_restore_max_entries must be 1..1000000 and state_restore_max_depth 1..256")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return cfg, errors.New("configuration contains trailing data")
	}
	return cfg, cfg.Validate()
}

// Validate enforces paths, explicit peer trust and bounded resource settings.
func (cfg Config) Validate() error {
	for name, path := range map[string]string{"app_dir": cfg.AppDir, "state_dir": cfg.StateDir, "run_dir": cfg.RunDir} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
			return fmt.Errorf("%s must be a clean absolute directory", name)
		}
	}
	if cfg.AppDir == cfg.StateDir || cfg.StateDir == cfg.RunDir || cfg.RunDir == cfg.AppDir {
		return errors.New("deployment directories must differ")
	}
	if !strings.HasPrefix(cfg.Prefix, "/app/") || strings.ContainsAny(cfg.Prefix, "?#%\\") || filepath.Clean(cfg.Prefix) != cfg.Prefix {
		return errors.New("prefix must be a clean /app/ path without a trailing slash")
	}
	if cfg.GatewayAccount == "" || cfg.GatewayAccount == "root" || len(cfg.TrustedGatewayAccounts) == 0 {
		return errors.New("gateway_account must be nonroot and trusted_gateway_accounts must be explicit")
	}
	if !slices.Contains(cfg.TrustedGatewayAccounts, "root") {
		return errors.New("trusted_gateway_accounts must include root for the package's local health probe")
	}
	if _, err := gatewaypolicy.New(cfg.GatewayPolicy()); err != nil {
		return fmt.Errorf("gateway policy: %w", err)
	}
	if cfg.MaxUsers < 1 || cfg.MaxUsers > 128 || cfg.MaxConnectionsPerUser < 1 || cfg.MaxConnectionsPerUser > 64 {
		return errors.New("max_users must be 1..128 and max_connections_per_user 1..64")
	}
	if cfg.ConnectionLifetimeSeconds < 60 || cfg.ConnectionLifetimeSeconds > 86400 {
		return errors.New("connection_lifetime_seconds must be 60..86400")
	}
	if cfg.StartupTimeoutSeconds < 5 || cfg.StartupTimeoutSeconds > 120 || cfg.ShutdownTimeoutSeconds < 10 || cfg.ShutdownTimeoutSeconds > 300 {
		return errors.New("startup_timeout_seconds must be 5..120 and shutdown_timeout_seconds 10..300")
	}
	for _, path := range strings.Split(cfg.UserPath, ":") {
		if !filepath.IsAbs(path) {
			return errors.New("user_path contains a relative or empty entry")
		}
	}
	if cfg.Language == "" || strings.ContainsAny(cfg.Language, "\x00\r\n=") {
		return errors.New("language must be a locale name")
	}
	if !filepath.IsAbs(cfg.TerminalShell) || filepath.Clean(cfg.TerminalShell) != cfg.TerminalShell {
		return errors.New("terminal_shell must be an absolute executable path")
	}
	return nil
}

// GatewayPolicy supplies the shared validator from the application's settings.
// Load resolves the default once; programmatic fixtures use the same default.
func (cfg Config) GatewayPolicy() gatewaypolicy.Config {
	allow := true
	if cfg.AllowSameOriginFetch != nil {
		allow = *cfg.AllowSameOriginFetch
	}
	lifetime, capacity, perUser := cfg.GatewayGrantLifetimeSeconds, cfg.GatewayGrantCapacity, cfg.GatewayGrantPerUser
	if lifetime == 0 {
		lifetime = defaultGatewayGrantLifetimeSeconds
	}
	if capacity == 0 {
		capacity = defaultGatewayGrantCapacity
	}
	if perUser == 0 {
		perUser = defaultGatewayGrantPerUser
	}
	return gatewaypolicy.Config{HandshakeGrants: &gatewaypolicy.GrantConfig{Lifetime: time.Duration(lifetime) * time.Second, Capacity: capacity, PerUser: perUser}, Prefix: cfg.Prefix, AllowedHosts: cfg.AllowedHosts,
		ExternalOriginPorts: cfg.ExternalOriginPorts, AllowedOrigins: cfg.AllowedOrigins,
		AllowSameOriginFetch: allow}
}

// Configure resolves the packaged path placeholders and creates a new root-only file.
// Existing configuration is preserved and validated, including local policy changes.
func Configure(template, output, appDir, stateDir, runDir string) error {
	if _, err := os.Stat(output); err == nil {
		_, err = Load(output)
		return err
	}
	data, err := os.ReadFile(template)
	if err != nil {
		return err
	}
	for token, value := range map[string]string{"@APP_DIR@": appDir, "@STATE_DIR@": stateDir, "@RUN_DIR@": runDir} {
		encoded, _ := json.Marshal(value)
		data = bytes.ReplaceAll(data, []byte(`"`+token+`"`), encoded)
	}
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		_, err = Load(output)
	}
	if err != nil {
		_ = os.Remove(output)
	}
	return err
}
