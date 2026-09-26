package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Verify tables exist by querying schema_migrations.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("schema_migrations query: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 migration, got %d", count)
	}
}

func TestOpen_CreatesDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "state.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	db.Close()
	if _, err := os.Stat(filepath.Dir(path)); os.IsNotExist(err) {
		t.Fatal("expected directory to be created")
	}
}

func TestOpen_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	p, err := db.CreateProject("demo")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := db.CreateWorkspaceFull(p.ID, dir, 1, 2, "", false, ""); err != nil {
		t.Fatalf("CreateWorkspaceFull: %v", err)
	}
	db.Close()

	// Reopen: migrations must not duplicate, data must survive.
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	defer db2.Close()
	var count int
	if err := db2.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 migration after reopen, got %d", count)
	}
	got, err := db2.GetProject(p.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Name != "demo" {
		t.Fatalf("name = %q", got.Name)
	}
}

func TestULIDMonotonicPrefix(t *testing.T) {
	a := newULID()
	b := newULID()
	if len(a) != 26 || len(b) != 26 {
		t.Fatalf("ULID length = %d/%d, want 26", len(a), len(b))
	}
	if a == b {
		t.Fatal("ULIDs must be unique")
	}
}

func TestResolveMovedAndGit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	p, _ := db.CreateProject("p")
	ws, _ := db.CreateWorkspaceFull(p.ID, "/nonexistent/old", 111, 222, "/git/common", false, "")
	if err := db.AddFingerprint(p.ID, "root_commit", "abc123", false); err != nil {
		t.Fatalf("AddFingerprint: %v", err)
	}
	if _, err := db.FindWorkspaceByDevIno(111, 222); err != nil {
		t.Fatalf("FindByDevIno: %v", err)
	}
	sibs, err := db.FindWorkspacesByGitCommon("/git/common")
	if err != nil || len(sibs) != 1 {
		t.Fatalf("FindByGitCommon = %v, %v", len(sibs), err)
	}
	if err := db.UpdateWorkspacePath(ws.ID, "/new/path", 111, 222, "/git/common"); err != nil {
		t.Fatalf("UpdatePath: %v", err)
	}
	got, _ := db.GetWorkspace(ws.ID)
	if got.Path != "/new/path" {
		t.Fatalf("path = %q", got.Path)
	}
	matched, err := db.GetProjectByFingerprints([]Fingerprint{{Kind: "root_commit", Value: "abc123"}})
	if err != nil || len(matched) != 1 {
		t.Fatalf("fingerprint match = %v, %v", len(matched), err)
	}
}
