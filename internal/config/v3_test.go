package config

import (
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
	// Repo file (trusted) + project file: project wins per-app fields.
	if err := os.WriteFile(filepath.Join(ws, "agent-runtime.yaml"),
		[]byte("apps:\n  api:\n    command: [repo-cmd]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "agent-runtime.yaml"),
		[]byte("apps:\n  api:\n    command: [proj-cmd]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, errs := Resolve(dir, "proj_1", "ws_1", ws, true, nil)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if got := res.Apps["api"].Command[0]; got != "proj-cmd" {
		t.Fatalf("command = %q, want proj-cmd", got)
	}
	if res.Provenance["apps.api"] != LayerProject {
		t.Fatalf("provenance = %q", res.Provenance["apps.api"])
	}
	// Untrusted repo file is skipped.
	res2, _ := Resolve(dir, "proj_1", "ws_1", ws, false, nil)
	if got := res2.Apps["api"].Command[0]; got != "proj-cmd" {
		t.Fatalf("untrusted command = %q", got)
	}
}
