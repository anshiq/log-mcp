package server

import (
	"context"
	"testing"
	"time"

	"agent-runtime/pkg/client"
)

// TestWatchProcesses_SnapshotNeverNull is the regression test for B3: a
// stream opened before any workspace has loaded must send an empty array,
// not a null snapshot (the UI store does `for (const p of msg.snapshot)`
// and a null value there is a runtime error client-side).
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

// TestWatchProcesses_LiveUpdatesCarryFullProcess is the regression test for
// B4: the daemon used to send {"kind":"upsert","event":"...","processId":"..."}
// with no "process" payload, so the UI store (which requires msg.process)
// silently dropped every live update after the initial snapshot. It also
// covers B3's second half: a process started in a workspace that had not
// been loaded yet when WatchProcesses opened must still appear live.
func TestWatchProcesses_LiveUpdatesCarryFullProcess(t *testing.T) {
	eng, done := testEngine(t)
	defer done()
	cl := client.New(sockPath(t, eng), "test/cli")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Open the watch stream before any workspace has been resolved, so the
	// only way this test can see the process below is via the
	// runtime-load subscription added for B3.
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
