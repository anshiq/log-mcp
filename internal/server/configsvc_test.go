package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"agent-runtime/pkg/client"
)

func TestConfigApply_StaleRevisionReturns409(t *testing.T) {
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

	applied, err := cl.ConfigService().Apply(ctx, map[string]any{
		"projectId": res.ProjectID, "layer": "project",
		"yaml": "apps:\n  api:\n    command: [sleep, \"5\"]\n", "message": "first",
	})
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if applied["applied"] != true {
		t.Fatalf("first apply not applied: %v", applied)
	}

	_, err = cl.ConfigService().Apply(ctx, map[string]any{
		"projectId": res.ProjectID, "layer": "project",
		"yaml": "apps:\n  api:\n    command: [sleep, \"10\"]\n", "message": "second",
		"baseRevision": 999999,
	})
	if err == nil {
		t.Fatal("expected an error applying against a stale base revision")
	}
	if !strings.Contains(err.Error(), "409") {
		t.Fatalf("expected a 409 status in the error, got: %v", err)
	}
}

func TestConfigApply_WorkspaceLayerWritesOverlay(t *testing.T) {
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

	applied, err := cl.ConfigService().Apply(ctx, map[string]any{
		"projectId": res.ProjectID, "workspaceId": res.WorkspaceID, "layer": "workspace",
		"yaml": "apps:\n  worker:\n    command: [sleep, \"5\"]\n", "message": "overlay",
	})
	if err != nil {
		t.Fatalf("apply workspace layer: %v", err)
	}
	if applied["applied"] != true {
		t.Fatalf("apply not applied: %v", applied)
	}

	data, _, err := eng.Store().GetConfig(res.ProjectID, "workspace", res.WorkspaceID)
	if err != nil {
		t.Fatalf("overlay not in db: %v", err)
	}
	if !strings.Contains(string(data), "worker") {
		t.Fatalf("overlay content = %q", data)
	}
}

func TestConfigPlan_ReportsStaleAgainstLatestRevision(t *testing.T) {
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
		"yaml": "apps:\n  api:\n    command: [sleep, \"5\"]\n", "message": "first",
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	var plan struct {
		Stale          bool  `json:"stale"`
		LatestRevision int64 `json:"latestRevision"`
	}
	if err := cl.Call(ctx, "ConfigService", "Plan", map[string]any{
		"workspaceId": res.WorkspaceID, "yaml": "apps:\n  api:\n    command: [sleep, \"9\"]\n",
		"baseRevision": 1,
	}, &plan); err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.LatestRevision == 0 {
		t.Fatal("expected a non-zero latestRevision")
	}
	if plan.Stale {
		t.Fatalf("base_revision matches latest; should not be stale: %+v", plan)
	}

	if err := cl.Call(ctx, "ConfigService", "Plan", map[string]any{
		"workspaceId": res.WorkspaceID, "yaml": "apps:\n  api:\n    command: [sleep, \"9\"]\n",
		"baseRevision": 999999,
	}, &plan); err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !plan.Stale {
		t.Fatalf("base_revision does not match latest; should be stale: %+v", plan)
	}
}
