package server

import (
	"context"
	"testing"
	"time"

	"agent-runtime/pkg/client"
)

func TestConfigResolveProposalApproveDismiss(t *testing.T) {
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
		"yaml": "apps:\n  api:\n    command: [sleep, \"5\"]\n", "message": "base",
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	propYAML := "apps:\n  api:\n    command: [sleep, \"9\"]\n"
	propJSON := `{"id":"abc123","yaml":` + quoteJSON(propYAML) + `,"summary":[{"app":"api","action":"update","reason":"test"}],"createdAt":1}`
	if err := eng.Store().SetProposal(res.ProjectID, propJSON); err != nil {
		t.Fatalf("set proposal: %v", err)
	}
	var approved map[string]any
	if err := cl.Call(ctx, "ConfigService", "ResolveProposal", map[string]any{
		"projectId": res.ProjectID, "proposalId": "abc123", "action": "approve",
	}, &approved); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved["applied"] != true {
		t.Fatalf("not applied: %v", approved)
	}
	if err := eng.Store().SetProposal(res.ProjectID, propJSON); err != nil {
		t.Fatal(err)
	}
	var dismissed map[string]any
	if err := cl.Call(ctx, "ConfigService", "ResolveProposal", map[string]any{
		"projectId": res.ProjectID, "proposalId": "abc123", "action": "dismiss",
	}, &dismissed); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	if dismissed["dismissed"] != true {
		t.Fatalf("not dismissed: %v", dismissed)
	}
}

func quoteJSON(s string) string {
	out := "\""
	for _, r := range s {
		switch r {
		case '"':
			out += "\\\""
		case '\n':
			out += "\\n"
		default:
			out += string(r)
		}
	}
	return out + "\""
}

func TestConfigGetReturnsProposalAndSource(t *testing.T) {
	eng, done := testEngine(t)
	defer done()
	cl := client.New(sockPath(t, eng), "test/cli")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws := t.TempDir()
	res, err := cl.ProjectService().Resolve(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	propJSON := `{"id":"p1","yaml":"apps: {}\n","summary":[],"createdAt":1}`
	if err := eng.Store().SetProposal(res.ProjectID, propJSON); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := cl.Call(ctx, "ConfigService", "GetConfig", map[string]any{"workspaceId": res.WorkspaceID}, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got["configSource"] != "db" {
		t.Fatalf("configSource = %v", got["configSource"])
	}
	if got["pendingProposal"] == nil {
		t.Fatalf("pendingProposal missing: %v", got)
	}
}
