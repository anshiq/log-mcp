package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"agent-runtime/internal/config"
	rtmcp "agent-runtime/internal/mcp"
	"agent-runtime/internal/runtime"
	"agent-runtime/pkg/api"
)

var (
	helperPath string
	binPath    string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "agent-runtime-mcp-helper-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	helperPath = filepath.Join(dir, "helper")
	cmd := exec.Command("go", "build", "-o", helperPath, "../process/testdata/helper")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build helper: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	// Build the real binary for the stdio end-to-end test.
	binPath = filepath.Join(dir, "agent-runtime")
	bcmd := exec.Command("go", "build", "-o", binPath, "../../cmd/agent-runtime")
	if out, err := bcmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build binary: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func newRuntime(t *testing.T) *runtime.Runtime {
	t.Helper()
	loaded, err := config.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return runtime.New(loaded, nil)
}

// connect builds a runtime + server and opens a client session over an
// in-memory transport.
func connect(t *testing.T) (*runtime.Runtime, *mcp.ClientSession) {
	t.Helper()
	rt := newRuntime(t)
	server := rtmcp.NewServer(rt, nil)
	clientT, serverT := mcp.NewInMemoryTransports()
	ctx := context.Background()
	go server.Run(ctx, serverT)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		rt.Shutdown()
		session.Close()
	})
	return rt, session
}

func call[T any](t *testing.T, session *mcp.ClientSession, tool string, args map[string]any) (T, bool) {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: tool, Arguments: args,
	})
	if err != nil {
		t.Fatalf("call %s: %v", tool, err)
	}
	if res.IsError {
		var zero T
		return zero, false
	}
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text = tc.Text
			break
		}
	}
	var out T
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal %s result %q: %v", tool, text, err)
	}
	return out, true
}

func startArgs(behaviour string) map[string]any {
	return map[string]any{
		"command": helperPath,
		"args":    []string{behaviour},
		"workdir": os.TempDir(),
	}
}

func TestStartProcess(t *testing.T) {
	_, session := connect(t)
	start := time.Now()
	res, ok := call[api.StartResult](t, session, "start_process", startArgs("ignore-term"))
	if !ok {
		t.Fatal("start_process failed")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("start_process took %v, must return immediately", time.Since(start))
	}
	if res.Status != "running" || res.ProcessID == "" {
		t.Fatalf("bad result: %+v", res)
	}
}

func TestStartProcessErrors(t *testing.T) {
	_, session := connect(t)
	if _, ok := call[api.StartResult](t, session, "start_process", map[string]any{}); ok {
		t.Fatal("expected error for empty start_process")
	}
	if _, ok := call[api.StartResult](t, session, "start_process", map[string]any{"app": "missing"}); ok {
		t.Fatal("expected error for unknown app")
	}
}

func TestStopProcess(t *testing.T) {
	_, session := connect(t)
	start, _ := call[api.StartResult](t, session, "start_process", startArgs("graceful"))
	res, ok := call[map[string]string](t, session, "stop_process", map[string]any{"process_id": start.ProcessID})
	if !ok || res["status"] != "stopped" {
		t.Fatalf("stop failed: %+v", res)
	}
	// Idempotent: stopping again is a no-op success.
	if _, ok := call[map[string]string](t, session, "stop_process", map[string]any{"process_id": start.ProcessID}); !ok {
		t.Fatal("second stop should succeed")
	}
	// Unknown id errors.
	if _, ok := call[map[string]string](t, session, "stop_process", map[string]any{"process_id": "nope"}); ok {
		t.Fatal("expected error for unknown process")
	}
}

func TestRestartProcess(t *testing.T) {
	_, session := connect(t)
	start, _ := call[api.StartResult](t, session, "start_process", startArgs("print"))
	res, ok := call[api.StartResult](t, session, "restart_process", map[string]any{"process_id": start.ProcessID})
	if !ok {
		t.Fatal("restart failed")
	}
	if res.ProcessID != start.ProcessID {
		t.Fatalf("logical id changed on restart: %s -> %s", start.ProcessID, res.ProcessID)
	}
	if res.InstanceID == start.InstanceID {
		t.Fatal("instance id did not change")
	}
	if res.Status != "running" {
		t.Fatalf("status = %q", res.Status)
	}
}

func TestProcessStatus(t *testing.T) {
	_, session := connect(t)
	start, _ := call[api.StartResult](t, session, "start_process", startArgs("print"))
	st, ok := call[api.StatusResult](t, session, "process_status", map[string]any{"process_id": start.ProcessID})
	if !ok {
		t.Fatal("status failed")
	}
	if st.PID <= 0 || st.Status != "running" {
		t.Fatalf("bad status: %+v", st)
	}
	if _, ok := call[api.StatusResult](t, session, "process_status", map[string]any{"process_id": "nope"}); ok {
		t.Fatal("expected error for unknown process")
	}
}

func TestListProcesses(t *testing.T) {
	_, session := connect(t)
	call[api.StartResult](t, session, "start_process", startArgs("ignore-term"))
	list, ok := call[api.ListResult](t, session, "list_processes", map[string]any{})
	if !ok || len(list.Processes) != 1 {
		t.Fatalf("bad list: %+v", list)
	}
}

func TestGetLogs(t *testing.T) {
	_, session := connect(t)
	start, _ := call[api.StartResult](t, session, "start_process", startArgs("print"))
	// poll until logs appear
	waitForLogs(t, session, start.ProcessID)
	logs, ok := call[api.GetLogsResult](t, session, "get_logs", map[string]any{
		"process_id": start.ProcessID, "lines": 100,
	})
	if !ok {
		t.Fatal("get_logs failed")
	}
	if len(logs.Entries) < 4 {
		t.Fatalf("expected >=4 entries, got %d", len(logs.Entries))
	}
	// stream filter
	stdout, _ := call[api.GetLogsResult](t, session, "get_logs", map[string]any{
		"process_id": start.ProcessID, "stream": "stdout",
	})
	for _, e := range stdout.Entries {
		if e.Stream != "stdout" {
			t.Fatalf("bad stream in stdout query: %+v", e)
		}
	}
	// contains filter
	filtered, _ := call[api.GetLogsResult](t, session, "get_logs", map[string]any{
		"process_id": start.ProcessID, "contains": "out-line-1",
	})
	if len(filtered.Entries) != 1 {
		t.Fatalf("contains filter: %d entries", len(filtered.Entries))
	}
	// unknown process errors
	if _, ok := call[api.GetLogsResult](t, session, "get_logs", map[string]any{"process_id": "nope"}); ok {
		t.Fatal("expected error")
	}
}

func waitForLogs(t *testing.T, session *mcp.ClientSession, pid string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		logs, ok := call[api.GetLogsResult](t, session, "get_logs", map[string]any{"process_id": pid, "lines": 100})
		if ok && len(logs.Entries) >= 4 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for logs")
}

func TestClearLogs(t *testing.T) {
	_, session := connect(t)
	start, _ := call[api.StartResult](t, session, "start_process", startArgs("once"))
	// Wait for the process to finish printing and exit, so nothing new arrives
	// after we clear.
	call[api.WaitForExitResult](t, session, "wait_for_exit", map[string]any{
		"process_id": start.ProcessID, "timeout_ms": 5000,
	})
	res, ok := call[api.ClearLogsResult](t, session, "clear_logs", map[string]any{"process_id": start.ProcessID})
	if !ok || !res.Cleared {
		t.Fatalf("clear failed: %+v", res)
	}
	logs, _ := call[api.GetLogsResult](t, session, "get_logs", map[string]any{"process_id": start.ProcessID})
	if len(logs.Entries) != 0 {
		t.Fatalf("logs not cleared: %d", len(logs.Entries))
	}
	if _, ok := call[api.ClearLogsResult](t, session, "clear_logs", map[string]any{"process_id": "nope"}); ok {
		t.Fatal("expected error")
	}
}

func TestSendStdin(t *testing.T) {
	_, session := connect(t)
	start, _ := call[api.StartResult](t, session, "start_process", startArgs("stdin-echo"))
	res, ok := call[api.SendStdinResult](t, session, "send_stdin", map[string]any{
		"process_id": start.ProcessID, "data": "hello\n",
	})
	if !ok || res.Written != 6 {
		t.Fatalf("send_stdin failed: %+v", res)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		logs, _ := call[api.GetLogsResult](t, session, "get_logs", map[string]any{"process_id": start.ProcessID})
		for _, e := range logs.Entries {
			if e.Line == "got: hello" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("echoed input not observed")
}

func TestWaitForLogTool(t *testing.T) {
	_, session := connect(t)
	start, _ := call[api.StartResult](t, session, "start_process", startArgs("print"))
	res, ok := call[api.WaitForLogResult](t, session, "wait_for_log", map[string]any{
		"process_id": start.ProcessID, "contains": "out-line-3", "timeout_ms": 5000,
	})
	if !ok {
		t.Fatal("wait_for_log failed")
	}
	if !res.Matched || res.Entry == nil || !strings.Contains(res.Entry.Line, "out-line-3") {
		t.Fatalf("bad wait result: %+v", res)
	}

	// timeout path
	res2, ok := call[api.WaitForLogResult](t, session, "wait_for_log", map[string]any{
		"process_id": start.ProcessID, "contains": "never", "timeout_ms": 200,
	})
	if !ok {
		t.Fatal("wait_for_log timeout call failed")
	}
	if res2.Matched || !res2.Timeout {
		t.Fatalf("expected timeout: %+v", res2)
	}

	// invalid matcher errors
	if _, ok := call[api.WaitForLogResult](t, session, "wait_for_log", map[string]any{"process_id": start.ProcessID}); ok {
		t.Fatal("expected error for no matcher")
	}
}

func TestWaitForExitTool(t *testing.T) {
	_, session := connect(t)
	start, _ := call[api.StartResult](t, session, "start_process", startArgs("stderr-exit"))
	res, ok := call[api.WaitForExitResult](t, session, "wait_for_exit", map[string]any{
		"process_id": start.ProcessID, "timeout_ms": 5000,
	})
	if !ok {
		t.Fatal("wait_for_exit failed")
	}
	if !res.Exited || res.ExitCode == nil || *res.ExitCode != 1 {
		t.Fatalf("bad exit result: %+v", res)
	}
}

func TestListAppsTool(t *testing.T) {
	loaded, err := config.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(loaded, nil)
	server := rtmcp.NewServer(rt, nil)
	clientT, serverT := mcp.NewInMemoryTransports()
	ctx := context.Background()
	go server.Run(ctx, serverT)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	res, ok := call[api.ListAppsResult](t, session, "list_apps", map[string]any{})
	if !ok {
		t.Fatal("list_apps failed")
	}
	if len(res.Apps) != 0 {
		t.Fatalf("expected no apps, got %+v", res.Apps)
	}
}

func TestRemoveProcessTool(t *testing.T) {
	_, session := connect(t)
	// Exited process: removable.
	once, _ := call[api.StartResult](t, session, "start_process", startArgs("once"))
	call[api.WaitForExitResult](t, session, "wait_for_exit", map[string]any{
		"process_id": once.ProcessID, "timeout_ms": 5000,
	})
	res, ok := call[api.RemoveProcessResult](t, session, "remove_process", map[string]any{"process_id": once.ProcessID})
	if !ok || !res.Removed {
		t.Fatalf("remove of exited process failed: %+v", res)
	}
	if _, ok := call[api.GetLogsResult](t, session, "get_logs", map[string]any{"process_id": once.ProcessID}); ok {
		t.Fatal("logs should be gone after remove_process")
	}
	// Running process: refused without force.
	running, _ := call[api.StartResult](t, session, "start_process", startArgs("ignore-term"))
	if _, ok := call[api.RemoveProcessResult](t, session, "remove_process", map[string]any{"process_id": running.ProcessID}); ok {
		t.Fatal("expected error removing a running process without force")
	}
	// Force removes a running process.
	res, ok = call[api.RemoveProcessResult](t, session, "remove_process", map[string]any{"process_id": running.ProcessID, "force": true})
	if !ok || !res.Removed {
		t.Fatalf("force remove failed: %+v", res)
	}
	list, _ := call[api.ListResult](t, session, "list_processes", map[string]any{})
	if len(list.Processes) != 0 {
		t.Fatalf("registry not empty after removals: %+v", list.Processes)
	}
	// Unknown id errors.
	if _, ok := call[api.RemoveProcessResult](t, session, "remove_process", map[string]any{"process_id": "nope"}); ok {
		t.Fatal("expected error for unknown process")
	}
}

func TestMalformedParameters(t *testing.T) {
	_, session := connect(t)
	// Non-object args should produce a tool error, not a crash.
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "process_status", Arguments: "not-an-object",
	})
	if err == nil {
		if res == nil || !res.IsError {
			t.Fatal("expected error for malformed arguments")
		}
	}
}

func TestConcurrentCalls(t *testing.T) {
	rt, session := connect(t)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name:      "start_process",
				Arguments: startArgs("print"),
			})
			if err != nil || res.IsError {
				t.Errorf("concurrent start %d failed: %v %v", i, err, res)
				return
			}
		}(i)
	}
	wg.Wait()
	list, _ := rt.List()
	if len(list.Processes) != 6 {
		t.Fatalf("expected 6 processes, got %d", len(list.Processes))
	}
}

// TestStdioServer exercises the real binary over a stdio MCP connection,
// covering the transport used by Claude Code / Codex / Gemini CLI.
func TestStdioServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stdio end-to-end in short mode")
	}
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1"}, nil)
	transport := &mcp.CommandTransport{Command: exec.Command(binPath, "serve")}
	transport.Command.Dir = t.TempDir()
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	start, ok := call[api.StartResult](t, session, "start_process", startArgs("print"))
	if !ok {
		t.Fatal("start_process failed over stdio")
	}
	if start.Status != "running" {
		t.Fatalf("status = %q", start.Status)
	}
	st, ok := call[api.StatusResult](t, session, "process_status", map[string]any{"process_id": start.ProcessID})
	if !ok || st.PID <= 0 {
		t.Fatalf("status failed over stdio: %+v", st)
	}
	// wait_for_exit for a one-shot process via stdio.
	call[api.StartResult](t, session, "start_process", startArgs("once"))
	list, ok := call[api.ListResult](t, session, "list_processes", map[string]any{})
	if !ok || len(list.Processes) < 2 {
		t.Fatalf("list over stdio: %+v", list)
	}
	// Instructions must be surfaced to clients (drives multi-agent UX).
	info := session.InitializeResult()
	if !strings.Contains(info.Instructions, "wait_for_log") {
		t.Fatalf("instructions missing workflow guide: %q", info.Instructions)
	}
}
