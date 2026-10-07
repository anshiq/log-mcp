package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigration2Tables(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, tbl := range []string{"project_configs", "project_config_sync"} {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM "+tbl).Scan(&n); err != nil {
			t.Fatalf("table %s missing: %v", tbl, err)
		}
	}
}

func TestPutConfigTxAtomicity(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := db.CreateProject("p")
	if err != nil {
		t.Fatal(err)
	}
	yaml1 := "apps:\n  api:\n    command: [a]\n"
	rev1, err := db.PutConfigTx(p.ID, "project", "", yaml1, "sha1", true, "", "auto", "", "first")
	if err != nil || rev1 == 0 {
		t.Fatalf("put1 = %d, %v", rev1, err)
	}
	if _, err := db.db.Exec("INSERT INTO config_revisions (project_id, layer, content, sha256, valid, errors_json, source, session_id, message, created_at) VALUES (NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)"); err == nil {
		t.Fatal("expected constraint error for null insert")
	}
	data, rev, err := db.GetConfig(p.ID, "project", "")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != yaml1 || rev != rev1 {
		t.Fatalf("head changed after failed insert: %q rev %d", data, rev)
	}
}

func TestImportLegacy(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	projDir := filepath.Join(dataDir, "projects", "proj_legacy")
	if err := os.MkdirAll(filepath.Join(projDir, "workspaces"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "agent-runtime.yaml"), []byte("apps:\n  api:\n    command: [a]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "workspaces", "ws_1.yaml"), []byte("apps:\n  w:\n    command: [b]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := db.CreateProject("legacy")
	if err != nil {
		t.Fatal(err)
	}
	_ = p
	renamed := filepath.Join(dataDir, "projects", p.ID)
	if err := os.Rename(projDir, renamed); err != nil {
		t.Fatal(err)
	}
	n, err := db.ImportLegacyConfigs(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("imported = %d", n)
	}
	data, _, err := db.GetConfig(p.ID, "project", "")
	if err != nil || len(data) == 0 {
		t.Fatalf("project config not imported: %q %v", data, err)
	}
}
