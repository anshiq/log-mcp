package server

import (
	"context"
	"testing"
	"time"

	"agent-runtime/pkg/client"
)

func TestConfigApply_RestartAffectedActuallyRestarts(t *testing.T) {
	eng, done := testEngine(t)
	defer done()
	cl := client.New(sockPath(t, eng), "test/cli")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	ws := t.TempDir()
	res, err := cl.ProjectService().Resolve(ctx, ws)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := cl.ConfigService().Apply(ctx, map[string]any{
		"projectId": res.ProjectID, "layer": "project",
		"yaml": "apps:\n  web:\n    command: [sleep, \"30\"]\n", "message": "v1",
	}); err != nil {
		t.Fatalf("apply v1: %v", err)
	}

	started, err := cl.ProcessService().Start(ctx, &client.StartRequest{
		WorkspaceID: res.WorkspaceID, App: "web",
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	before, err := cl.ProcessService().Get(ctx, started.ProcessID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	applied, err := cl.ConfigService().Apply(ctx, map[string]any{
		"projectId": res.ProjectID, "layer": "project",
		"yaml": "apps:\n  web:\n    command: [sleep, \"31\"]\n", "message": "v2",
		"restartAffected": true,
	})
	if err != nil {
		t.Fatalf("apply v2: %v", err)
	}
	restarted, _ := applied["restarted"].([]any)
	found := false
	for _, r := range restarted {
		if r == started.ProcessID {
			found = true
		}
	}
	if !found {
		t.Fatalf("restarted list = %v, want to contain %q", restarted, started.ProcessID)
	}

	after, err := cl.ProcessService().Get(ctx, started.ProcessID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if after["instance_id"] == before["instance_id"] {
		t.Fatalf("instance_id unchanged (%v); process was not actually restarted", after["instance_id"])
	}
}
