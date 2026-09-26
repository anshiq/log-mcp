package migrate

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"agent-runtime/internal/core"
	"agent-runtime/internal/store"

	_ "modernc.org/sqlite"
)

func TestMigrateLegacyArchive(t *testing.T) {
	dataDir := t.TempDir()
	db, err := store.Open(filepath.Join(dataDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	eng := core.NewWithOptions(db, core.Options{DataDir: dataDir})
	defer eng.Close()

	// Legacy project layout: repo yaml + .agent-runtime/logs.db + audit.log.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "agent-runtime.yaml"),
		[]byte("apps:\n  api:\n    command: [npm, run, dev]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacyDir := filepath.Join(root, ".agent-runtime")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy, err := sql.Open("sqlite", filepath.Join(legacyDir, "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`CREATE TABLE entries(process_id TEXT, id INTEGER, ts INTEGER, stream TEXT, line TEXT)`);
		err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		if _, err := legacy.Exec(`INSERT INTO entries VALUES (?,?,?,?,?)`,
			"proc_old", i, 1000+i, "stdout", "legacy line"); err != nil {
			t.Fatal(err)
		}
	}
	legacy.Close()
	if err := os.WriteFile(filepath.Join(legacyDir, "audit.log"),
		[]byte("{\"tool\":\"start_process\",\"args\":{},\"result\":\"ok\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := Migrate(eng, dataDir, root, Options{Yes: true})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if rep.ConfigsImported != 1 {
		t.Fatalf("configs = %d", rep.ConfigsImported)
	}
	if rep.LogsImported != 5 {
		t.Fatalf("logs = %d", rep.LogsImported)
	}
	if rep.AuditImported != 1 {
		t.Fatalf("audit = %d", rep.AuditImported)
	}
	// Config file landed in the central store.
	pid, _, err := eng.ResolveWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "projects", string(pid), "agent-runtime.yaml")); err != nil {
		t.Fatalf("central config missing: %v", err)
	}
}
