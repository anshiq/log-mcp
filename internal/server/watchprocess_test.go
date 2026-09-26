package server

import (
	"context"
	"testing"
	"time"

	"agent-runtime/pkg/client"
)

func TestWatchProcesses_SnapshotNeverNull(t *testing.T) {
	eng, done := testEngine(t)
	defer done()
	cl := client.New(sockPath(t, eng), "test/cli")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := cl.ProcessService().Watch(ctx, "", true)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	msg := <-ch
	if msg["kind"] != "snapshot" {
		t.Fatalf("first message kind = %v, want snapshot", msg["kind"])
	}
	if msg["snapshot"] == nil {
		t.Fatal("snapshot is nil; must be an empty array on a fresh daemon")
	}
	snap, ok := msg["snapshot"].([]any)
	if !ok {
		t.Fatalf("snapshot is %T, want []any", msg["snapshot"])
	}
	if len(snap) != 0 {
		t.Fatalf("snapshot = %v, want empty", snap)
	}
}

func TestWatchProcesses_LiveUpdatesCarryFullProcess(t *testing.T) {
	eng, done := testEngine(t)
	defer done()
	cl := client.New(sockPath(t, eng), "test/cli")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	ch, err := cl.ProcessService().Watch(ctx, "", true)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if msg := <-ch; msg["kind"] != "snapshot" {
		t.Fatalf("first message kind = %v, want snapshot", msg["kind"])
	}

	ws := t.TempDir()
	res, err := cl.ProjectService().Resolve(ctx, ws)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	started, err := cl.ProcessService().Start(ctx, &client.StartRequest{
		WorkspaceID: res.WorkspaceID,
		Command:     []string{"sh", "-c", "sleep 30"},
		WorkDir:     ws,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	deadline := time.After(15 * time.Second)
	for {
		select {
		case msg := <-ch:
			if msg["kind"] != "upsert" {
				continue
			}
			if msg["processId"] != started.ProcessID {
				continue
			}
			proc, ok := msg["process"].(map[string]any)
			if !ok {
				t.Fatalf("upsert has no process payload: %v", msg)
			}
			if proc["id"] != started.ProcessID {
				t.Fatalf("process.id = %v, want %v", proc["id"], started.ProcessID)
			}
			if proc["status"] == nil || proc["status"] == "" {
				t.Fatalf("process.status missing: %v", proc)
			}
			return
		case <-deadline:
			t.Fatal("never saw a live upsert for the newly started process")
		}
	}
}
