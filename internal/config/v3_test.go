package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateUnknownKey(t *testing.T) {
	errs := Validate([]byte("version: 3\nbogus_key: 1\n"))
	if len(errs) == 0 {
		t.Fatal("expected error for unknown key")
	}
}

func TestValidateLifetime(t *testing.T) {
	errs := Validate([]byte("apps:\n  api:\n    lifetime: forever\n"))
	if len(errs) == 0 {
		t.Fatal("expected error for bad lifetime")
	}
	if errs := Validate([]byte("apps:\n  api:\n    command: [npm, run, dev]\n    lifetime: session\n")); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestValidateUnknownAppField(t *testing.T) {
	errs := Validate([]byte("apps:\n  web:\n    command: [sleep, \"100\"]\n    bogus: 1\n"))
	if len(errs) == 0 {
		t.Fatal("expected an error for the unknown apps.web.bogus field")
	}
	found := false
	for _, e := range errs {
		if e.Line > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected at least one error with a line number, got %+v", errs)
	}
}

func TestPlanRestartVsLive(t *testing.T) {
	oldApps := map[string]V3App{
		"api": {AppConfig: AppConfig{Command: []string{"npm", "run", "dev"}}},
	}
	newApps := map[string]V3App{
		"api": {AppConfig: AppConfig{Command: []string{"npm", "run", "start"}}},
	}
	changes := Plan(oldApps, newApps, map[string][]string{"api": {"proc_1"}})
	if len(changes) != 1 || changes[0].Kind != ChangeRestartRequired {
		t.Fatalf("changes = %+v", changes)
	}
	live := map[string]V3App{
		"api": {AppConfig: AppConfig{Command: []string{"npm", "run", "dev"}, Restart: Restart{Policy: "always"}}},
	}
	changes = Plan(oldApps, live, nil)
	if len(changes) != 1 || changes[0].Kind != ChangeAppliedLive {
		t.Fatalf("live changes = %+v", changes)
	}
}

func TestResolveLayering(t *testing.T) {
	dir := t.TempDir()
	ws := t.TempDir()
	projDir := filepath.Join(dir, "projects", "proj_1")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "agent-runtime.yaml"),
		[]byte("apps:\n  api:\n    command: [proj-cmd]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wsDir := filepath.Join(dir, "projects", "proj_1", "workspaces")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "ws_1.yaml"),
		[]byte("apps:\n  api:\n    command: [ws-cmd]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, errs := Resolve(dir, "proj_1", "ws_1", ws, nil)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if got := res.Apps["api"].Command[0]; got != "ws-cmd" {
		t.Fatalf("command = %q, want ws-cmd", got)
	}
	if res.Provenance["apps.api"] != LayerWorkspace {
		t.Fatalf("provenance = %q", res.Provenance["apps.api"])
	}
}

func TestV3JSONSchemaRaw_ParsesAsCompleteObject(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(V3JSONSchemaRaw(), &schema); err != nil {
		t.Fatalf("embedded schema is invalid JSON: %v", err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no properties object: %v", schema)
	}
	for _, key := range []string{"version", "project", "runtime", "apps", "logging", "alerts"} {
		if _, ok := props[key]; !ok {
			t.Errorf("schema.properties missing %q", key)
		}
	}
}
