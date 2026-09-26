package server

import (
	"context"
	"testing"
	"time"

	"agent-runtime/pkg/client"
)

func TestTailLogs_NoDuplicateBacklogReplay(t *testing.T) {
	eng, done := testEngine(t)
	defer done()
	cl := client.New(sockPath(t, eng), "test/cli")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	ws := t.TempDir()
	res, err := cl.ProjectService().Resolve(ctx, ws)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	started, err := cl.ProcessService().Start(ctx, &client.StartRequest{
		WorkspaceID: res.WorkspaceID,
		Command:     []string{"sh", "-c", "for i in 1 2 3; do echo line-$i; done; sleep 30"},
		WorkDir:     ws,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	ch, err := cl.LogService().Tail(ctx, map[string]any{
		"processIds": []string{started.ProcessID}, "backlog": 100,
	})
	if err != nil {
		t.Fatalf("tail: %v", err)
	}

	seen := map[string]int{}
	timeout := time.After(3 * time.Second)
collect:
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				break collect
			}
			switch msg["kind"] {
			case "batch":
				lines, _ := msg["lines"].([]any)
				for _, l := range lines {
					m, _ := l.(map[string]any)
					line, _ := m["line"].(string)
					seen[line]++
				}
			case "line":
				line, _ := msg["line"].(string)
				seen[line]++
			}
		case <-timeout:
			break collect
		}
	}

	for _, want := range []string{"line-1", "line-2", "line-3"} {
		if seen[want] != 1 {
			t.Errorf("line %q seen %d times, want exactly 1 (duplicate backlog replay)", want, seen[want])
		}
	}
}
