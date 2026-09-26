package server

import (
	"context"
	"testing"
	"time"

	"agent-runtime/pkg/client"
)

func TestProcessStart_StoreRowKeyedByRuntimeID(t *testing.T) {
	eng, done := testEngine(t)
	defer done()
	cl := client.New(sockPath(t, eng), "test/cli")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ws := t.TempDir()
	res, err := cl.ProjectService().Resolve(ctx, ws)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := cl.ConfigService().Apply(ctx, map[string]any{
		"projectId": res.ProjectID, "layer": "project",
		"yaml": "apps:\n  web:\n    command: [sleep, \"30\"]\n", "message": "apps",
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	started, err := cl.ProcessService().Start(ctx, &client.StartRequest{
		WorkspaceID: res.WorkspaceID,
		App:         "web",
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	row, err := eng.Store().GetProcess(started.ProcessID)
	if err != nil {
		t.Fatalf("GetProcess(%q): %v (store row must be keyed by the runtime's process id)", started.ProcessID, err)
	}
	if row.App != "web" {
		t.Fatalf("row.App = %q, want %q", row.App, "web")
	}

	ch, err := cl.ProcessService().Watch(ctx, res.WorkspaceID, false)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	msg := <-ch
	snap, _ := msg["snapshot"].([]any)
	if len(snap) != 1 {
		t.Fatalf("snapshot = %v", snap)
	}
	p, _ := snap[0].(map[string]any)
	if p["app"] != "web" {
		t.Fatalf("snapshot process.app = %v, want %q", p["app"], "web")
	}
}
