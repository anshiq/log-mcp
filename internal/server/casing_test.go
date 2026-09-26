package server

import (
	"context"
	"testing"
	"time"

	"agent-runtime/pkg/client"
)

func TestCasingCompat_CamelCaseAcceptedByPkgAPIHandlers(t *testing.T) {
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
	started, err := cl.ProcessService().Start(ctx, &client.StartRequest{
		WorkspaceID: res.WorkspaceID,
		Command:     []string{"sh", "-c", "echo hi; sleep 30"},
		WorkDir:     ws,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := started.ProcessID

	var getLogs struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := cl.Call(ctx, "LogService", "GetLogs",
		map[string]any{"processId": pid, "lines": 5}, &getLogs); err != nil {
		t.Fatalf("GetLogs with camelCase processId: %v", err)
	}

	var getEnv map[string]any
	if err := cl.Call(ctx, "ProcessService", "GetEnv",
		map[string]any{"processId": pid}, &getEnv); err != nil {
		t.Fatalf("GetEnv with camelCase processId: %v", err)
	}

	var restartPolicy map[string]any
	if err := cl.Call(ctx, "ProcessService", "SetRestartPolicy",
		map[string]any{"processId": pid, "policy": "always"}, &restartPolicy); err != nil {
		t.Fatalf("SetRestartPolicy with camelCase processId: %v", err)
	}

	var waitExit map[string]any
	if err := cl.Call(ctx, "ProcessService", "WaitForExit",
		map[string]any{"processId": pid, "timeoutMs": 50}, &waitExit); err != nil {
		t.Fatalf("WaitForExit with camelCase processId/timeoutMs: %v", err)
	}
	if waitExit["timeout"] != true {
		t.Fatalf("expected a timeout (still running): %v", waitExit)
	}
}

func TestCasingConversion_SnakeAndCamel(t *testing.T) {
	got := toSnakeCase("processId")
	if got != "process_id" {
		t.Fatalf("toSnakeCase(processId) = %q, want process_id", got)
	}
	got = toCamelCase("process_id")
	if got != "processId" {
		t.Fatalf("toCamelCase(process_id) = %q, want processId", got)
	}
	if toSnakeCase("workspaceId") != "workspace_id" {
		t.Fatalf("toSnakeCase(workspaceId) = %q", toSnakeCase("workspaceId"))
	}
	if toCamelCase("timeout_ms") != "timeoutMs" {
		t.Fatalf("toCamelCase(timeout_ms) = %q", toCamelCase("timeout_ms"))
	}
}
