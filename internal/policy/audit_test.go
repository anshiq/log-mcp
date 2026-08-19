package policy

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAuditRecordAndQuery(t *testing.T) {
	a, err := NewAudit(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	a.Record("start_process", map[string]any{"app": "api"}, "ok", 12*time.Millisecond, "mcp")
	a.Record("get_logs", map[string]any{"process_id": "p1"}, "ok", 3*time.Millisecond, "mcp")
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := a.Query(0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("query returned %d entries, want 2", len(entries))
	}
	if entries[0].Tool != "start_process" || entries[1].Tool != "get_logs" {
		t.Fatalf("unexpected ordering: %+v", entries)
	}
	if entries[0].DurationMS != 12 {
		t.Fatalf("duration_ms = %d, want 12", entries[0].DurationMS)
	}
	if entries[1].Caller != "mcp" {
		t.Fatalf("caller = %q, want mcp", entries[1].Caller)
	}
}

func TestAuditQueryContainsAndLimit(t *testing.T) {
	a, err := NewAudit(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		a.Record("start_process", nil, "ok", time.Millisecond, "mcp")
	}
	a.Record("signal_process", nil, "ok", time.Millisecond, "mcp")
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	only, err := a.Query(0, "signal_process")
	if err != nil {
		t.Fatal(err)
	}
	if len(only) != 1 || only[0].Tool != "signal_process" {
		t.Fatalf("contains filter returned %+v, want 1 signal_process", only)
	}

	two, err := a.Query(2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(two) != 2 {
		t.Fatalf("limit returned %d entries, want 2", len(two))
	}
	if two[0].Tool != "start_process" || two[1].Tool != "signal_process" {
		t.Fatalf("tail = %+v, want start_process, signal_process", two)
	}
}

func TestAuditRotation(t *testing.T) {
	dir := t.TempDir()
	a, err := newAudit(dir, nil, 2048, 5) // 2KB files, 5 total
	if err != nil {
		t.Fatal(err)
	}
	// ~170-byte entries: each 2KB file holds ~12, so 40 entries force several
	// rotations while the 5-file capacity (10KB) still retains every entry.
	for i := 0; i < 40; i++ {
		a.Record("start_process", map[string]any{
			"i":       i,
			"app":     "api",
			"payload": strings.Repeat("x", 50),
		}, "ok", time.Millisecond, "mcp")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	rotated := 0
	for i := 1; i <= 4; i++ {
		if _, err := os.Stat(filepath.Join(dir, "audit.log."+strconv.Itoa(i))); err == nil {
			rotated++
		}
	}
	if rotated == 0 {
		t.Fatal("expected at least one rotated audit.log.N file")
	}

	entries, err := a.Query(0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 40 {
		t.Fatalf("query returned %d entries across rotated files, want 40", len(entries))
	}
	// Entries survive rotation oldest-to-newest without loss or duplication.
	prev := -1
	for _, e := range entries {
		if e.Tool != "start_process" {
			t.Fatalf("unexpected tool %q", e.Tool)
		}
		idx := int(e.Args["i"].(float64))
		if idx <= prev {
			t.Fatalf("entry order broken after rotation: %d after %d", idx, prev)
		}
		prev = idx
	}
	if prev != 39 {
		t.Fatalf("newest entry lost in rotation (last index %d)", prev)
	}
	// Total files on disk must respect the cap (audit.log + 4 rotated).
	files, _ := filepath.Glob(filepath.Join(dir, "audit.log*"))
	if len(files) > 5 {
		t.Fatalf("%d audit files on disk, cap is 5", len(files))
	}
}

func TestAuditRedactsSecretArgs(t *testing.T) {
	a, err := NewAudit(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	a.Record("start_process", map[string]any{
		"command": "go run .",
		"env":     map[string]any{"API_TOKEN": "sekrit", "PATH": "/usr/bin"},
		"headers": []any{map[string]any{"password": "hunter2"}},
	}, "ok", time.Millisecond, "mcp")
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := a.Query(0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("query returned %d entries, want 1", len(entries))
	}
	env := entries[0].Args["env"].(map[string]any)
	if env["API_TOKEN"] != "***" {
		t.Fatalf("API_TOKEN not redacted: %v", env["API_TOKEN"])
	}
	if env["PATH"] != "/usr/bin" {
		t.Fatalf("non-secret PATH was modified: %v", env["PATH"])
	}
	headers := entries[0].Args["headers"].([]any)
	if headers[0].(map[string]any)["password"] != "***" {
		t.Fatalf("nested password not redacted")
	}
	if entries[0].Args["command"] != "go run ." {
		t.Fatalf("non-secret command was modified: %v", entries[0].Args["command"])
	}
}

func TestAuditNewDirFailure(t *testing.T) {
	parent := t.TempDir()
	file := filepath.Join(parent, "blocked")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAudit(filepath.Join(file, "sub"), nil); err == nil {
		t.Fatal("expected error creating audit dir under a regular file")
	}
}

func TestAuditRecordAfterCloseIsNoop(t *testing.T) {
	a, err := NewAudit(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	// Must not panic or block.
	a.Record("start_process", map[string]any{"app": "api"}, "ok", time.Millisecond, "mcp")
	entries, err := a.Query(0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("entry recorded after Close, want none")
	}
}

func TestAuditRecordQueueFullDropsGracefully(t *testing.T) {
	a, err := newAudit(t.TempDir(), nil, 1<<20, 5)
	if err != nil {
		t.Fatal(err)
	}
	// Fill the queue faster than the writer drains: Record must never block.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5000; i++ {
			a.Record("start_process", map[string]any{"app": "api"}, "ok", time.Millisecond, "mcp")
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Record blocked on a full queue")
	}
	a.Close()
}
