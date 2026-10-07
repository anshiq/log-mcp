package core

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"agent-runtime/internal/config"
	"agent-runtime/internal/store"
)

func testSetup(t *testing.T) (*store.DB, *ProjectRuntime, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	p, err := db.CreateProject("test")
	if err != nil {
		t.Fatal(err)
	}
	wsPath := t.TempDir()
	ws, err := db.CreateWorkspace(p.ID, wsPath)
	if err != nil {
		t.Fatal(err)
	}
	pr := &ProjectRuntime{
		wsID:      ws.ID,
		projectID: p.ID,
		wsPath:    wsPath,
		dataDir:   dir,
		logger:    slog.Default(),
		store:     db,
		stale:     map[string]bool{},
	}
	return db, pr, wsPath
}

func writePkg(t *testing.T, dir, scripts string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(scripts), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureSeedsEmptyProject(t *testing.T) {
	db, pr, wsPath := testSetup(t)
	name := filepath.Base(wsPath)
	writePkg(t, wsPath, `{"scripts":{"dev":"next dev"}}`)
	EnsureConfig(pr)
	data, _, err := db.GetConfig(pr.projectID, "project", "")
	if err != nil || len(data) == 0 {
		t.Fatalf("not seeded: %q %v", data, err)
	}
	cfg, errs := config.ParseV3(data)
	if errs != nil {
		t.Fatalf("errs %v", errs)
	}
	if _, ok := cfg.Apps[name]; !ok {
		t.Fatalf("expected app %q in %v", name, cfg.Apps)
	}
	revs, _ := db.ListRevisions(pr.projectID, "project", 5)
	found := false
	for _, r := range revs {
		if r.Source == "auto" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no auto revision")
	}
}

func TestEnsureAutoUpdateAndRemove(t *testing.T) {
	db, pr, wsPath := testSetup(t)
	writePkg(t, wsPath, `{"scripts":{"dev":"node a"}}`)
	EnsureConfig(pr)
	writePkg(t, wsPath, `{"scripts":{"start":"node b"}}`)
	EnsureConfig(pr)
	data, _, _ := db.GetConfig(pr.projectID, "project", "")
	cfg, _ := config.ParseV3(data)
	found := false
	for _, app := range cfg.Apps {
		for _, c := range app.Command {
			if c == "start" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("auto update did not apply: %s", data)
	}
	if err := os.Remove(filepath.Join(wsPath, "package.json")); err != nil {
		t.Fatal(err)
	}
	EnsureConfig(pr)
	data, _, _ = db.GetConfig(pr.projectID, "project", "")
	cfg, _ = config.ParseV3(data)
	if len(cfg.Apps) != 0 {
		t.Fatalf("auto remove did not apply: %s", data)
	}
}

func TestEnsureUserEditProposalNoOverwrite(t *testing.T) {
	db, pr, wsPath := testSetup(t)
	writePkg(t, wsPath, `{"scripts":{"dev":"node a"}}`)
	EnsureConfig(pr)
	data, _, _ := db.GetConfig(pr.projectID, "project", "")
	cfg, _ := config.ParseV3(data)
	var appName string
	for k := range cfg.Apps {
		appName = k
		break
	}
	edited := "version: 3\napps:\n  " + appName + ":\n    command: [mycmd]\n"
	rev, _, err := (&Engine{store: db}).ApplyProjectYAML(pr.projectID, "project", "", edited, "api", "", "user edit", false)
	if err != nil || rev == 0 {
		t.Fatalf("manual apply %v", err)
	}
	writePkg(t, wsPath, `{"scripts":{"dev":"node b"}}`)
	EnsureConfig(pr)
	data2, _, _ := db.GetConfig(pr.projectID, "project", "")
	if string(data2) != edited {
		t.Fatalf("user app overwritten: got %q want %q", data2, edited)
	}
	syncRow, _ := db.GetConfigSync(pr.projectID)
	if syncRow.ProposalJSON == "" {
		t.Fatalf("expected proposal")
	}
	var prop map[string]any
	if err := json.Unmarshal([]byte(syncRow.ProposalJSON), &prop); err != nil {
		t.Fatal(err)
	}
	if _, ok := prop["yaml"]; !ok {
		t.Fatalf("proposal missing yaml: %v", prop)
	}
}

func TestDismissAndApprove(t *testing.T) {
	db, pr, wsPath := testSetup(t)
	writePkg(t, wsPath, `{"scripts":{"dev":"node a"}}`)
	EnsureConfig(pr)
	data, _, _ := db.GetConfig(pr.projectID, "project", "")
	cfg, _ := config.ParseV3(data)
	var appName string
	for k := range cfg.Apps {
		appName = k
	}
	edited := "version: 3\napps:\n  " + appName + ":\n    command: [mycmd]\n"
	e := &Engine{store: db}
	if _, _, err := e.ApplyProjectYAML(pr.projectID, "project", "", edited, "api", "", "user", false); err != nil {
		t.Fatal(err)
	}
	writePkg(t, wsPath, `{"scripts":{"dev":"node b"}}`)
	EnsureConfig(pr)
	syncRow, _ := db.GetConfigSync(pr.projectID)
	if syncRow.ProposalJSON == "" {
		t.Fatalf("expected proposal")
	}
	_ = db.ClearProposal(pr.projectID)
	sig := "test-sig-dismiss"
	_ = db.PutConfigSync(pr.projectID, sig, syncRow.AutoAppsJSON)
	syncRow2, _ := db.GetConfigSync(pr.projectID)
	if syncRow2.ProposalJSON != "" {
		t.Fatalf("dismiss did not clear")
	}
	if syncRow2.RunSignature != sig {
		t.Fatalf("dismiss did not store signature")
	}
	var prop struct {
		YAML string `json:"yaml"`
	}
	_ = json.Unmarshal([]byte(syncRow.ProposalJSON), &prop)
	if _, _, err := e.ApplyProjectYAML(pr.projectID, "project", "", prop.YAML, "proposal", "", "approve", false); err != nil {
		t.Fatal(err)
	}
	data2, _, _ := db.GetConfig(pr.projectID, "project", "")
	if string(data2) != prop.YAML {
		t.Fatalf("approve did not apply")
	}
}
