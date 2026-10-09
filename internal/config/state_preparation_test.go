package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStatePreparationBudgetsRejectUnboundedTraversal(t *testing.T) {
	data, err := os.ReadFile("../../packaging/fnos/app/etc/gateway.json")
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	values["app_dir"], values["state_dir"], values["run_dir"] = "/opt/fntuios", "/var/lib/fntuios", "/run/fntuios"
	delete(values, "state_restore_max_entries")
	delete(values, "state_restore_max_depth")
	path := filepath.Join(t.TempDir(), "gateway.json")
	load := func() (Config, error) {
		body, _ := json.Marshal(values)
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
		return Load(path)
	}
	cfg, err := load()
	if err != nil || cfg.StateRestoreMaxEntries != 100000 || cfg.StateRestoreMaxDepth != 64 {
		t.Fatalf("absent fields did not preserve deployability: %+v, %v", cfg, err)
	}
	for key, limits := range map[string][]int{"state_restore_max_entries": {0, -1, 1000001}, "state_restore_max_depth": {0, -1, 257}} {
		values[key] = nil
		if _, err := load(); err == nil {
			t.Fatalf("null traversal budget %s accepted", key)
		}
		for _, value := range limits {
			values[key] = value
			if _, err := load(); err == nil {
				t.Fatalf("unsafe traversal budget %s=%d accepted", key, value)
			}
		}
		delete(values, key)
	}
	values["state_restore_max_entries"], values["state_restore_max_depth"] = 75000, 32
	cfg, err = load()
	if err != nil || cfg.StateRestoreMaxEntries != 75000 || cfg.StateRestoreMaxDepth != 32 {
		t.Fatalf("editable traversal limits were not preserved: %v", err)
	}
}
