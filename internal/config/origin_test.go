package config

import (
	"encoding/json"
	"os"
	"testing"
)

// Deployment origin configuration cannot silently widen browser authority on a typo.
func TestOriginPortPolicyValidation(t *testing.T) {
	data, err := os.ReadFile("../../packaging/fnos/app/etc/gateway.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.AppDir = "/opt/fntuios"
	cfg.StateDir = "/var/lib/fntuios"
	cfg.RunDir = "/run/fntuios"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, policy := range []map[string][]uint16{
		{"*": {5666}}, {"HTTP": {5666}}, {"ws": {5666}}, {"http": {0}}, {"https": {5667, 5667}},
	} {
		cfg.ExternalOriginPorts = policy
		if err := cfg.Validate(); err == nil {
			t.Fatalf("unsafe policy accepted: %v", policy)
		}
	}
	cfg.ExternalOriginPorts = map[string][]uint16{}
	if err := cfg.Validate(); err != nil {
		t.Fatal("empty policy must disable fallback", err)
	}
}

// Missing deployment fields adopt the documented repair; an explicit opt-out
// and exact-origin exceptions must survive the owning configuration loader.
func TestGatewayPolicyConfiguration(t *testing.T) {
	data, err := os.ReadFile("../../packaging/fnos/app/etc/gateway.json")
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	values["app_dir"], values["state_dir"], values["run_dir"] = "/opt/fntuios", "/var/lib/fntuios", "/run/fntuios"
	delete(values, "allow_same_origin_fetch")
	file := t.TempDir() + "/gateway.json"
	load := func() (Config, error) {
		body, _ := json.Marshal(values)
		if err := os.WriteFile(file, body, 0600); err != nil {
			t.Fatal(err)
		}
		return Load(file)
	}
	cfg, err := load()
	if err != nil || cfg.AllowSameOriginFetch == nil || !*cfg.AllowSameOriginFetch {
		t.Fatal("missing field did not resolve new default", err)
	}
	values["allow_same_origin_fetch"] = false
	values["allowed_origins"] = []string{"https://public.example:9443"}
	cfg, err = load()
	if err != nil || cfg.AllowSameOriginFetch == nil || *cfg.AllowSameOriginFetch {
		t.Fatal("explicit opt-out was lost", err)
	}
	if cfg.GatewayGrantLifetimeSeconds != 30 || cfg.GatewayGrantCapacity != 512 {
		t.Fatal("grant defaults were not resolved by the configuration loader")
	}
	values["gateway_grant_lifetime_seconds"] = 0
	if _, err := load(); err == nil {
		t.Fatal("zero grant lifetime accepted")
	}
	values["gateway_grant_lifetime_seconds"] = 30
	values["gateway_grant_capacity"] = 8193
	if _, err := load(); err == nil {
		t.Fatal("unbounded grant capacity accepted")
	}
	values["gateway_grant_capacity"] = 512
	values["allowed_origins"] = []string{"https://*.example"}
	if _, err := load(); err == nil {
		t.Fatal("wildcard origin accepted")
	}
}
