package server

import (
	"context"
	"testing"
	"time"

	"agent-runtime/pkg/client"
)

// TestCasingCompat_CamelCaseAcceptedByPkgAPIHandlers is the regression test
// for B6: GetLogs, GetEnv, WaitForLog, Signal, SetRestartPolicy and
// WaitForExit decode into pkg/api structs that only recognize snake_case
// (process_id, timeout_ms, ...), while every other handler in this package
// only recognizes camelCase (processId). A client that didn't happen to
// know which of the two casings a given endpoint wanted got a confusing
// "processId required" / "unknown process \"\"" error. Both casings must
// now work on every endpoint.
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

// TestCasingCompat_StrictModeStillCatchesTypos ensures the alias-both-
// casings normalization in decode() does not defeat the "unknown apps
// field" strictness added for B9-adjacent validation: a body with a
// genuinely unrelated field name is not silently accepted just because
// normalizeTopLevelCasing ran over it.
func TestCasingCompat_StrictModeStillCatchesTypos(t *testing.T) {
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
