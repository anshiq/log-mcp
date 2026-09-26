// Integration test for the v3 daemon core: two workspaces through one
// engine — start in one, see from the other scope, restart survives.
package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agent-runtime/internal/core"
	"agent-runtime/internal/store"
	"agent-runtime/pkg/client"
)

func testEngine(t *testing.T) (*core.Engine, func()) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	eng := core.NewWithOptions(db, core.Options{DataDir: dir})
	srv, err := New(eng, filepath.Join(dir, "agentd.sock"), "test")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Serve(ctx) }()
	// Wait for the socket.
	for i := 0; i < 100; i++ {
		if conn, err := net.Dial("unix", filepath.Join(dir, "agentd.sock")); err == nil {
			conn.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	return eng, func() {
		cancel()
		_ = srv.Close()
		_ = eng.Close()
		_ = db.Close()
	}
}

func sockPath(t *testing.T, eng *core.Engine) string {
	t.Helper()
	// The test server socket lives next to state.db.
	return filepath.Join(eng.DataDir(), "agentd.sock")
}

func TestDaemonCore_StartSeeStop(t *testing.T) {
	eng, done := testEngine(t)
	defer done()
	cl := client.New(sockPath(t, eng), "test/cli")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Two sessions in two different workspaces.
	wsA := t.TempDir()
	wsB := t.TempDir()
	resA, err := cl.ProjectService().Resolve(ctx, wsA)
	if err != nil {
		t.Fatalf("resolve A: %v", err)
	}
	resB, err := cl.ProjectService().Resolve(ctx, wsB)
	if err != nil {
		t.Fatalf("resolve B: %v", err)
	}
	if resA.WorkspaceID == resB.WorkspaceID {
		t.Fatal("distinct dirs must resolve to distinct workspaces")
	}

	// Start a process in A via raw command.
	started, err := cl.ProcessService().Start(ctx, &client.StartRequest{
		WorkspaceID: resA.WorkspaceID,
		Command:     []string{"sh", "-c", "echo hello-v3 && sleep 30"},
		WorkDir:     wsA,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if started.ProcessID == "" {
		t.Fatal("empty process id")
	}

	// Get + List from the same workspace.
	got, err := cl.ProcessService().Get(ctx, started.ProcessID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fmt.Sprint(got["process_id"]) != started.ProcessID {
		t.Fatalf("get process_id = %v", got["process_id"])
	}
	list, err := cl.ProcessService().List(ctx, resA.WorkspaceID, false)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, %v", len(list), err)
	}

	// Logs contain the echo.
	var found bool
	for i := 0; i < 50 && !found; i++ {
		logs, err := cl.LogService().GetLogs(ctx, started.ProcessID, 50)
		if err != nil {
			t.Fatalf("logs: %v", err)
		}
		if entries, ok := logs["entries"].([]any); ok {
			for _, e := range entries {
				if m, ok := e.(map[string]any); ok {
					if s, _ := m["line"].(string); containsStr(s, "hello-v3") {
						found = true
					}
				}
			}
		}
		if !found {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if !found {
		t.Fatal("echo line never appeared in logs")
	}

	// Global IDs work across workspaces: Get from B's scope via all=true.
	all, err := cl.ProcessService().List(ctx, "", true)
	if err != nil || len(all) < 1 {
		t.Fatalf("all-workspaces list = %v, %v", len(all), err)
	}

	// Stop and remove.
	if err := cl.ProcessService().Stop(ctx, started.ProcessID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := cl.ProcessService().Remove(ctx, started.ProcessID, false); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// Config apply + revisions roundtrip.
	_ = os.MkdirAll(wsA, 0o755)
	applied, err := cl.ConfigService().Apply(ctx, map[string]any{
		"projectId": resA.ProjectID, "layer": "project",
		"yaml":    "apps:\n  api:\n    command: [npm, run, dev]\n",
		"message": "test",
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied["applied"] != true {
		t.Fatalf("applied = %v", applied)
	}
	revs, err := cl.ConfigService().Revisions(ctx, resA.ProjectID)
	if err != nil || len(revs) != 1 {
		t.Fatalf("revisions = %v, %v", len(revs), err)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
