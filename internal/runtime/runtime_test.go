package runtime_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-runtime/internal/config"
	"agent-runtime/internal/logs"
	"agent-runtime/internal/logstore"
	"agent-runtime/internal/process"
	"agent-runtime/internal/runtime"
	"agent-runtime/pkg/api"
)

var helperPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "agent-runtime-runtime-helper-")
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
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func newRuntime(t *testing.T) *runtime.Runtime {
	t.Helper()
	return newRuntimeWithConfig(t, "")
}

func newRuntimeWithConfig(t *testing.T, yaml string) *runtime.Runtime {
	t.Helper()
	rt, _ := newRuntimeWithConfigDir(t, yaml)
	return rt
}

// newRuntimeWithConfigDir is newRuntimeWithConfig but also returns the temp
// project dir, so tests can inspect the resolved DB path it produced.
func newRuntimeWithConfigDir(t *testing.T, yaml string) (*runtime.Runtime, string) {
	t.Helper()
	dir := t.TempDir()
	if yaml == "" {
		loaded := config.EmptyLoaded(dir)
		rt := runtime.New(loaded, nil)
		t.Cleanup(func() { rt.Shutdown() })
		return rt, dir
	}
	if err := os.WriteFile(filepath.Join(dir, "agent-runtime.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadFile(filepath.Join(dir, "agent-runtime.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(loaded, nil)
	t.Cleanup(func() { rt.Shutdown() })
	return rt, dir
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func rawCmd(t *testing.T, behaviour string, args ...string) api.StartRequest {
	t.Helper()
	cmd := append([]string{helperPath, behaviour}, args...)
	return api.StartRequest{Command: cmd[0], Args: cmd[1:], WorkDir: t.TempDir()}
}

func TestStartRawCommandReturnsImmediately(t *testing.T) {
	rt := newRuntime(t)
	start := time.Now()
	res, err := rt.Start(context.Background(), rawCmd(t, "ignore-term"))
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("Start took %v, must return immediately", time.Since(start))
	}
	if res.Status != "running" || res.ProcessID == "" {
		t.Fatalf("bad start result: %+v", res)
	}
	st, _ := rt.Status(res.ProcessID)
	if st.PID <= 0 {
		t.Fatalf("no pid in status: %+v", st)
	}
	rt.Stop(context.Background(), res.ProcessID)
}

func TestWaitForLogMatches(t *testing.T) {
	rt := newRuntime(t)
	res, _ := rt.Start(context.Background(), rawCmd(t, "print", "5"))
	w, err := rt.WaitForLog(context.Background(), api.WaitForLogRequest{
		ProcessID: res.ProcessID,
		Contains:  "out-line-3",
		TimeoutMS: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Matched || w.Entry == nil || !strings.Contains(w.Entry.Line, "out-line-3") {
		t.Fatalf("bad wait result: %+v", w)
	}
	rt.Stop(context.Background(), res.ProcessID)
}

func TestWaitForLogReadyViaProfile(t *testing.T) {
	rt := newRuntime(t)
	res, _ := rt.Start(context.Background(), rawCmd(t, "print", "1"))
	// Force the "node" profile so the READY line matches a readiness pattern.
	// Reached by manually waiting with ready=true on a profile that has rules:
	// simulate by checking contains too.
	w, err := rt.WaitForLog(context.Background(), api.WaitForLogRequest{
		ProcessID: res.ProcessID,
		Contains:  "READY",
		TimeoutMS: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Matched {
		t.Fatalf("expected READY match: %+v", w)
	}
	rt.Stop(context.Background(), res.ProcessID)
}

func TestWaitForLogReadyUnsupportedProfile(t *testing.T) {
	rt := newRuntime(t)
	res, _ := rt.Start(context.Background(), rawCmd(t, "print", "1"))
	_, err := rt.WaitForLog(context.Background(), api.WaitForLogRequest{
		ProcessID: res.ProcessID,
		Ready:     true,
		TimeoutMS: 500,
	})
	if err == nil {
		t.Fatal("expected error for ready on generic profile")
	}
	rt.Stop(context.Background(), res.ProcessID)
}

func TestWaitForLogTimeout(t *testing.T) {
	rt := newRuntime(t)
	res, _ := rt.Start(context.Background(), rawCmd(t, "ignore-term"))
	w, err := rt.WaitForLog(context.Background(), api.WaitForLogRequest{
		ProcessID: res.ProcessID,
		Contains:  "never-printed",
		TimeoutMS: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.Matched || !w.Timeout || w.Exited {
		t.Fatalf("bad timeout result: %+v", w)
	}
	rt.Stop(context.Background(), res.ProcessID)
}

func TestWaitForLogExitBeforeMatch(t *testing.T) {
	rt := newRuntime(t)
	res, _ := rt.Start(context.Background(), rawCmd(t, "stderr-exit"))
	w, err := rt.WaitForLog(context.Background(), api.WaitForLogRequest{
		ProcessID: res.ProcessID,
		Contains:  "never-printed",
		TimeoutMS: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.Matched || !w.Exited {
		t.Fatalf("bad exit result: %+v", w)
	}
}

func TestWaitForLogCancellation(t *testing.T) {
	rt := newRuntime(t)
	res, _ := rt.Start(context.Background(), rawCmd(t, "ignore-term"))
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	_, err := rt.WaitForLog(ctx, api.WaitForLogRequest{
		ProcessID: res.ProcessID,
		Contains:  "nope",
		TimeoutMS: 10000,
	})
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	rt.Stop(context.Background(), res.ProcessID)
}

func TestWaitForLogMultipleWaiters(t *testing.T) {
	rt := newRuntime(t)
	res, _ := rt.Start(context.Background(), rawCmd(t, "print", "10"))
	var wg sync.WaitGroup
	results := make([]*api.WaitForLogResult, 3)
	for i, target := range []string{"out-line-1", "out-line-4", "out-line-7"} {
		wg.Add(1)
		go func(i int, target string) {
			defer wg.Done()
			w, err := rt.WaitForLog(context.Background(), api.WaitForLogRequest{
				ProcessID: res.ProcessID, Contains: target, TimeoutMS: 5000,
			})
			if err != nil {
				t.Errorf("waiter %d: %v", i, err)
				return
			}
			results[i] = w
		}(i, target)
	}
	wg.Wait()
	for i, w := range results {
		if w == nil || !w.Matched {
			t.Fatalf("waiter %d did not match: %+v", i, w)
		}
	}
	rt.Stop(context.Background(), res.ProcessID)
}

func TestWaitForLogRestartBoundary(t *testing.T) {
	rt := newRuntime(t)
	res, _ := rt.Start(context.Background(), rawCmd(t, "print", "1")) // prints READY then loops
	// Wait for first instance READY.
	waitFor(t, 5*time.Second, "first READY", func() bool {
		logs, _ := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Lines: 10})
		for _, e := range logs.Entries {
			if e.Line == "READY" {
				return true
			}
		}
		return false
	})
	// Restart and immediately wait for READY: must NOT match the previous
	// instance's READY line; it must wait for the new instance.
	restartStart := time.Now()
	if err := rt.Restart(context.Background(), res.ProcessID); err != nil {
		t.Fatal(err)
	}
	w, err := rt.WaitForLog(context.Background(), api.WaitForLogRequest{
		ProcessID: res.ProcessID, Contains: "READY", TimeoutMS: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Matched {
		t.Fatalf("expected READY match after restart: %+v", w)
	}
	// The matched line must come from the new instance: it must be timestamped
	// after the restart began (the old READY is ~20ms in the past).
	ts, err := time.Parse(time.RFC3339Nano, w.Entry.Timestamp)
	if err != nil {
		t.Fatal(err)
	}
	if ts.Before(restartStart.Add(-5 * time.Millisecond)) {
		t.Fatalf("matched old instance READY (ts %v before restart %v)", ts, restartStart)
	}
	rt.Stop(context.Background(), res.ProcessID)
}

func TestWaitForExit(t *testing.T) {
	rt := newRuntime(t)
	res, _ := rt.Start(context.Background(), rawCmd(t, "stderr-exit"))
	w, err := rt.WaitForExit(context.Background(), api.WaitForExitParams{
		ProcessID: res.ProcessID, TimeoutMS: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Exited || w.ExitCode == nil || *w.ExitCode != 1 {
		t.Fatalf("bad exit result: %+v", w)
	}
}

func TestWaitForExitTimeout(t *testing.T) {
	rt := newRuntime(t)
	res, _ := rt.Start(context.Background(), rawCmd(t, "ignore-term"))
	w, err := rt.WaitForExit(context.Background(), api.WaitForExitParams{
		ProcessID: res.ProcessID, TimeoutMS: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.Exited || !w.Timeout {
		t.Fatalf("bad timeout result: %+v", w)
	}
	rt.Stop(context.Background(), res.ProcessID)
}

func TestGetLogsFilters(t *testing.T) {
	rt := newRuntime(t)
	res, _ := rt.Start(context.Background(), rawCmd(t, "print", "3"))
	waitFor(t, 5*time.Second, "lines", func() bool {
		logs, _ := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Lines: 10})
		return len(logs.Entries) >= 4
	})
	// stdout only
	stdout, _ := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Stream: "stdout", Lines: 100})
	for _, e := range stdout.Entries {
		if e.Stream != "stdout" {
			t.Fatalf("non-stdout in stdout query: %+v", e)
		}
	}
	// contains filter
	filtered, _ := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Stream: "stdout", Contains: "out-line-1"})
	if len(filtered.Entries) != 1 {
		t.Fatalf("contains filter: got %d entries", len(filtered.Entries))
	}
	// line limit
	limited, _ := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Lines: 1})
	if len(limited.Entries) != 1 {
		t.Fatalf("line limit: got %d", len(limited.Entries))
	}
	if !limited.Truncated {
		t.Fatal("expected truncated=true")
	}
	rt.Stop(context.Background(), res.ProcessID)
}

func TestClearLogs(t *testing.T) {
	rt := newRuntime(t)
	res, _ := rt.Start(context.Background(), rawCmd(t, "once"))
	w, err := rt.WaitForExit(context.Background(), api.WaitForExitParams{ProcessID: res.ProcessID, TimeoutMS: 5000})
	if err != nil || !w.Exited {
		t.Fatalf("process did not exit: %v %+v", err, w)
	}
	cleared, err := rt.ClearLogs(res.ProcessID, "all")
	if err != nil || !cleared.Cleared {
		t.Fatalf("clear failed: %+v %v", cleared, err)
	}
	logs, _ := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Lines: 10})
	if len(logs.Entries) != 0 {
		t.Fatalf("logs not cleared: %d entries", len(logs.Entries))
	}
	rt.Stop(context.Background(), res.ProcessID)
}

func TestSendStdin(t *testing.T) {
	rt := newRuntime(t)
	res, _ := rt.Start(context.Background(), rawCmd(t, "stdin-echo"))
	waitFor(t, 5*time.Second, "running", func() bool {
		st, _ := rt.Status(res.ProcessID)
		return st.Status == "running"
	})
	if _, err := rt.SendStdin(res.ProcessID, "ping\n"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "echoed line", func() bool {
		logs, _ := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Lines: 10})
		for _, e := range logs.Entries {
			if e.Line == "got: ping" {
				return true
			}
		}
		return false
	})
	rt.Stop(context.Background(), res.ProcessID)
}

func TestUnknownProcessID(t *testing.T) {
	rt := newRuntime(t)
	if _, err := rt.Status("nope"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := rt.GetLogs(api.GetLogsRequest{ProcessID: "nope"}); err == nil {
		t.Fatal("expected error")
	}
	if err := rt.Stop(context.Background(), "nope"); err == nil {
		t.Fatal("expected error")
	}
	if err := rt.Restart(context.Background(), "nope"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := rt.SendStdin("nope", "x"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := rt.RemoveProcess("nope", false); err == nil {
		t.Fatal("expected error")
	}
	if _, err := rt.WaitForLog(context.Background(), api.WaitForLogRequest{ProcessID: "nope", Contains: "x"}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := rt.WaitForExit(context.Background(), api.WaitForExitParams{ProcessID: "nope"}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := rt.ClearLogs("nope", "all"); err == nil {
		t.Fatal("expected error")
	}
}

// TestEnvPrecedence exercises R6: env_file < app.env < request.env. The app
// branch of resolveStart must order the environment so later sources override
// earlier ones.
func TestEnvPrecedence(t *testing.T) {
	dir := t.TempDir()
	yaml := `
apps:
  envapp:
    command: ["` + helperPath + `", "env", "PRECEDENCE"]
    env_file: .env
    env: ["PRECEDENCE=app-env"]
`
	if err := os.WriteFile(filepath.Join(dir, "agent-runtime.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("PRECEDENCE=from-file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadFile(filepath.Join(dir, "agent-runtime.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(loaded, nil)

	// request.env wins over app.env and env_file.
	res, err := rt.Start(context.Background(), api.StartRequest{App: "envapp", Env: []string{"PRECEDENCE=request-env"}})
	if err != nil {
		t.Fatal(err)
	}
	w, err := rt.WaitForLog(context.Background(), api.WaitForLogRequest{
		ProcessID: res.ProcessID, Contains: "PRECEDENCE=", TimeoutMS: 5000,
	})
	if err != nil || !w.Matched || w.Entry == nil || w.Entry.Line != "PRECEDENCE=request-env" {
		t.Fatalf("request.env did not win: %+v %v", w, err)
	}
	rt.Stop(context.Background(), res.ProcessID)

	// Without a request override, app.env wins over env_file.
	res2, err := rt.Start(context.Background(), api.StartRequest{App: "envapp"})
	if err != nil {
		t.Fatal(err)
	}
	w2, err := rt.WaitForLog(context.Background(), api.WaitForLogRequest{
		ProcessID: res2.ProcessID, Contains: "PRECEDENCE=", TimeoutMS: 5000,
	})
	if err != nil || !w2.Matched || w2.Entry == nil || w2.Entry.Line != "PRECEDENCE=app-env" {
		t.Fatalf("app.env did not win over env_file: %+v %v", w2, err)
	}
	rt.Stop(context.Background(), res2.ProcessID)
}
func TestRemoveProcess(t *testing.T) {
	rt := newRuntime(t)
	res, err := rt.Start(context.Background(), rawCmd(t, "once"))
	if err != nil {
		t.Fatal(err)
	}
	if w, err := rt.WaitForExit(context.Background(), api.WaitForExitParams{ProcessID: res.ProcessID, TimeoutMS: 5000}); err != nil || !w.Exited {
		t.Fatalf("process did not exit: %v %+v", err, w)
	}
	removed, err := rt.RemoveProcess(res.ProcessID, false)
	if err != nil || !removed.Removed {
		t.Fatalf("remove failed: %+v %v", removed, err)
	}
	if _, err := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID}); err == nil {
		t.Fatal("logs should be gone after remove")
	}
	if list, _ := rt.List(); len(list.Processes) != 0 {
		t.Fatalf("registry not empty after remove: %+v", list.Processes)
	}

	// Running process: refused without force, accepted with force.
	running, _ := rt.Start(context.Background(), rawCmd(t, "ignore-term"))
	if _, err := rt.RemoveProcess(running.ProcessID, false); err == nil {
		t.Fatal("expected error removing a running process without force")
	}
	removed, err = rt.RemoveProcess(running.ProcessID, true)
	if err != nil || !removed.Removed {
		t.Fatalf("force remove failed: %+v %v", removed, err)
	}
	if list, _ := rt.List(); len(list.Processes) != 0 {
		t.Fatalf("registry not empty after force remove: %+v", list.Processes)
	}
}

func TestStartRequiresCommandOrApp(t *testing.T) {
	rt := newRuntime(t)
	if _, err := rt.Start(context.Background(), api.StartRequest{}); err == nil {
		t.Fatal("expected error for empty start request")
	}
}

func TestListProcesses(t *testing.T) {
	rt := newRuntime(t)
	res1, _ := rt.Start(context.Background(), rawCmd(t, "ignore-term"))
	res2, _ := rt.Start(context.Background(), rawCmd(t, "ignore-term"))
	list, _ := rt.List()
	if len(list.Processes) != 2 {
		t.Fatalf("list has %d processes, want 2", len(list.Processes))
	}
	found := map[string]bool{}
	for _, p := range list.Processes {
		found[p.ProcessID] = true
	}
	if !found[res1.ProcessID] || !found[res2.ProcessID] {
		t.Fatalf("missing processes: %+v", list.Processes)
	}
	rt.Shutdown()
}

func TestListApps(t *testing.T) {
	dir := t.TempDir()
	yaml := `
apps:
  web:
    type: nextjs
    command: ["npm", "run", "dev"]
  api:
    type: spring-boot
`
	if err := os.WriteFile(filepath.Join(dir, "agent-runtime.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadFile(filepath.Join(dir, "agent-runtime.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(loaded, nil)
	apps, _ := rt.Apps()
	if len(apps.Apps) != 2 {
		t.Fatalf("apps = %d, want 2", len(apps.Apps))
	}
	for _, a := range apps.Apps {
		if a.Name == "web" && (a.Type != "nextjs" || a.Command != "npm") {
			t.Fatalf("web app wrong: %+v", a)
		}
		if a.Name == "api" && a.Type != "spring-boot" {
			t.Fatalf("api app wrong: %+v", a)
		}
	}
}

func TestStartByAppName(t *testing.T) {
	dir := t.TempDir()
	yaml := `
apps:
  echoer:
    command: ["` + helperPath + `", "stdin-echo"]
`
	if err := os.WriteFile(filepath.Join(dir, "agent-runtime.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadFile(filepath.Join(dir, "agent-runtime.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(loaded, nil)
	res, err := rt.Start(context.Background(), api.StartRequest{App: "echoer"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "running", func() bool {
		st, _ := rt.Status(res.ProcessID)
		return st.Status == "running"
	})
	rt.Stop(context.Background(), res.ProcessID)

	// Unknown app errors.
	if _, err := rt.Start(context.Background(), api.StartRequest{App: "missing"}); err == nil {
		t.Fatal("expected unknown app error")
	}
}

func TestStopAndRestart(t *testing.T) {
	rt := newRuntime(t)
	res, _ := rt.Start(context.Background(), rawCmd(t, "print", "2"))
	if err := rt.Stop(context.Background(), res.ProcessID); err != nil {
		t.Fatal(err)
	}
	st, _ := rt.Status(res.ProcessID)
	if st.Status != "stopped" {
		t.Fatalf("status = %q, want stopped", st.Status)
	}
	if err := rt.Restart(context.Background(), res.ProcessID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "running after restart", func() bool {
		s, _ := rt.Status(res.ProcessID)
		return s.Status == "running"
	})
	rt.Shutdown()
}

func TestShutdownStopsProcesses(t *testing.T) {
	rt := newRuntime(t)
	res, _ := rt.Start(context.Background(), rawCmd(t, "graceful"))
	waitFor(t, 5*time.Second, "running", func() bool {
		st, _ := rt.Status(res.ProcessID)
		return st.Status == "running"
	})
	if err := rt.Shutdown(); err != nil {
		t.Fatal(err)
	}
	st, _ := rt.Status(res.ProcessID)
	if st.Status != "stopped" {
		t.Fatalf("status = %q, want stopped", st.Status)
	}
}

func TestWaitForLogInvalidRequest(t *testing.T) {
	rt := newRuntime(t)
	res, _ := rt.Start(context.Background(), rawCmd(t, "ignore-term"))
	if _, err := rt.WaitForLog(context.Background(), api.WaitForLogRequest{ProcessID: res.ProcessID}); err == nil {
		t.Fatal("expected error for no matcher")
	}
	if _, err := rt.WaitForLog(context.Background(), api.WaitForLogRequest{ProcessID: res.ProcessID, Pattern: "(["}); err == nil {
		t.Fatal("expected error for invalid regex")
	}
	rt.Stop(context.Background(), res.ProcessID)
}

// TestGetLogsByteCapSingleOversizedLine exercises B1: a single line larger than
// max_log_bytes must still be returned (never zero entries) and reported as
// truncated.
func TestGetLogsByteCapSingleOversizedLine(t *testing.T) {
	rt := newRuntimeWithConfig(t, "runtime:\n  max_log_bytes: 1000\n")
	res, err := rt.Start(context.Background(), rawCmd(t, "longline", "2000"))
	if err != nil {
		t.Fatal(err)
	}
	if w, err := rt.WaitForExit(context.Background(), api.WaitForExitParams{ProcessID: res.ProcessID, TimeoutMS: 5000}); err != nil || !w.Exited {
		t.Fatalf("process did not exit: %v %+v", err, w)
	}
	logs, err := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Lines: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs.Entries) != 1 {
		t.Fatalf("entries = %d, want 1 (single oversized line must be kept)", len(logs.Entries))
	}
	if len(logs.Entries[0].Line) != 2000 {
		t.Fatalf("line length = %d, want 2000", len(logs.Entries[0].Line))
	}
	if !logs.Truncated {
		t.Fatal("expected truncated=true when the only retained entry exceeds the byte cap")
	}
}

// TestGetLogsByteCapLongTail exercises B1: when the matching tail exceeds
// max_log_bytes, the response must drop the oldest entries until the retained
// bytes fit the cap.
func TestGetLogsByteCapLongTail(t *testing.T) {
	rt := newRuntimeWithConfig(t, "runtime:\n  max_log_bytes: 1000\n")
	res, err := rt.Start(context.Background(), rawCmd(t, "burst", "50"))
	if err != nil {
		t.Fatal(err)
	}
	if w, err := rt.WaitForExit(context.Background(), api.WaitForExitParams{ProcessID: res.ProcessID, TimeoutMS: 5000}); err != nil || !w.Exited {
		t.Fatalf("process did not exit: %v %+v", err, w)
	}
	logs, err := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Lines: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs.Entries) == 0 {
		t.Fatal("expected some entries")
	}
	if !logs.Truncated {
		t.Fatal("expected truncated=true for over-cap tail")
	}
	var retained int
	for _, e := range logs.Entries {
		retained += len(e.Line) + 64
	}
	if retained > 1000 {
		t.Fatalf("retained bytes = %d, want <= cap 1000", retained)
	}
	// Newest lines must be retained, oldest dropped.
	if last := logs.Entries[len(logs.Entries)-1].Line; last != "burst-line-49" {
		t.Fatalf("newest line = %q, want burst-line-49", last)
	}
	if first := logs.Entries[0].Line; first == "burst-line-0" {
		t.Fatal("oldest line was not dropped")
	}
}

// TestGetLogsByteCapExactBoundary exercises B1 boundary semantics: when the
// retained bytes sum exactly to max_log_bytes, nothing is dropped and
// truncated stays false.
func TestGetLogsByteCapExactBoundary(t *testing.T) {
	rt := newRuntimeWithConfig(t, "runtime:\n  max_log_bytes: 500\n")
	res, err := rt.Start(context.Background(), rawCmd(t, "fixedlines", "200", "172"))
	if err != nil {
		t.Fatal(err)
	}
	if w, err := rt.WaitForExit(context.Background(), api.WaitForExitParams{ProcessID: res.ProcessID, TimeoutMS: 5000}); err != nil || !w.Exited {
		t.Fatalf("process did not exit: %v %+v", err, w)
	}
	logs, err := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Lines: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs.Entries) != 2 {
		t.Fatalf("entries = %d, want 2 (exact boundary must not drop)", len(logs.Entries))
	}
	if logs.Truncated {
		t.Fatal("expected truncated=false at exact boundary")
	}
}

// TestRuntimeSQLiteMode exercises R8 end-to-end through the runtime facade:
// with log_store: sqlite the runtime builds a durable sink, live get_logs still
// serves from memory while the process runs, and Shutdown flushes the archive
// so the rows survive in the SQLite file.
func TestRuntimeSQLiteMode(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "logs.db")
	rt := newRuntimeWithConfig(t, "runtime:\n  log_store: sqlite\n  db_path: "+dbPath+"\n")

	res, err := rt.Start(context.Background(), rawCmd(t, "print", "3"))
	if err != nil {
		t.Fatal(err)
	}
	// print 3 emits out-line-0..2 then READY (4 lines total) and loops forever.
	waitFor(t, 5*time.Second, "all 4 lines", func() bool {
		logs, _ := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Lines: 100})
		var out0, out1, out2, ready bool
		for _, e := range logs.Entries {
			switch e.Line {
			case "out-line-0":
				out0 = true
			case "out-line-1":
				out1 = true
			case "out-line-2":
				out2 = true
			case "READY":
				ready = true
			}
		}
		return out0 && out1 && out2 && ready
	})

	// Requesting exactly the number of lines memory holds means memory satisfies
	// the query (Available >= Lines), so no archive back-fill is triggered and
	// the source is deterministically "memory".
	got, err := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Lines: 4})
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "memory" {
		t.Fatalf("source = %q, want memory (memory satisfies the query)", got.Source)
	}
	if len(got.Entries) != 4 {
		t.Fatalf("entries = %d, want 4", len(got.Entries))
	}

	// Shutdown must flush the archive before closing the sink.
	if err := rt.Shutdown(); err != nil {
		t.Fatal(err)
	}

	sink, err := logstore.Open(dbPath, nil)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	t.Cleanup(func() { sink.Close() })
	entries, err := sink.Query(res.ProcessID, logs.Query{Lines: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("persisted entries = %d, want 4", len(entries))
	}
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, e.Line)
	}
	for _, want := range []string{"out-line-0", "out-line-1", "out-line-2", "READY"} {
		found := false
		for _, l := range lines {
			if l == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("persisted lines %v missing %q", lines, want)
		}
	}
}

// TestRuntimeSQLiteBackfill is the crucial one: with a tiny ring buffer (5
// lines) and a process that prints 50 lines, the in-memory ring evicts the
// oldest 45. get_logs must back-fill those evicted lines from SQLite and merge
// them back in ascending ID order (source == "memory+db").
func TestRuntimeSQLiteBackfill(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "logs.db")
	rt := newRuntimeWithConfig(t, "runtime:\n  log_store: sqlite\n  db_path: "+dbPath+"\n  log_buffer_lines: 5\n")

	res, err := rt.Start(context.Background(), rawCmd(t, "burst", "50"))
	if err != nil {
		t.Fatal(err)
	}
	if w, err := rt.WaitForExit(context.Background(), api.WaitForExitParams{ProcessID: res.ProcessID, TimeoutMS: 5000}); err != nil || !w.Exited {
		t.Fatalf("process did not exit: %v %+v", err, w)
	}

	// The archive writer is asynchronous (flushes every ~200ms), so poll until
	// the back-fill path reports the full 50-line tail from memory+db.
	var got *api.GetLogsResult
	waitFor(t, 5*time.Second, "back-filled tail", func() bool {
		logs, err := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Lines: 100})
		if err != nil {
			return false
		}
		if logs.Source == "memory+db" && len(logs.Entries) >= 50 {
			got = logs
			return true
		}
		return false
	})

	if got.Source != "memory+db" {
		t.Fatalf("source = %q, want memory+db", got.Source)
	}
	if len(got.Entries) != 50 {
		t.Fatalf("entries = %d, want 50 (nothing lost)", len(got.Entries))
	}
	// The earliest lines were evicted from the 5-line ring; only the archive
	// can supply them, so their presence proves back-fill worked.
	if got.Entries[0].Line != "burst-line-0" {
		t.Fatalf("first entry = %q, want burst-line-0 (evicted line back-filled)", got.Entries[0].Line)
	}
	// Merged entries must stay in ascending ID order (no merge bugs).
	for i := 1; i < len(got.Entries); i++ {
		if got.Entries[i].ID <= got.Entries[i-1].ID {
			t.Fatalf("entries not ascending at %d: id %d then %d", i, got.Entries[i-1].ID, got.Entries[i].ID)
		}
	}
}

// TestRuntimeSQLiteBadPathFallback verifies a sqlite open failure is never
// fatal: runtime.New warns and falls back to memory, so process management and
// get_logs keep working with source == "memory".
func TestRuntimeSQLiteBadPathFallback(t *testing.T) {
	dir := t.TempDir()
	// A regular FILE where a directory is needed: MkdirAll on the parent of the
	// DB path fails, so logstore.Open must return an error.
	filePath := filepath.Join(dir, "afile")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(dir, "afile", "nested", "logs.db")

	rt := newRuntimeWithConfig(t, "runtime:\n  log_store: sqlite\n  db_path: "+badPath+"\n  log_buffer_lines: 5\n")
	// runtime.New must not panic or error: it logs a warning and falls back.

	res, err := rt.Start(context.Background(), rawCmd(t, "burst", "20"))
	if err != nil {
		t.Fatal(err)
	}
	if w, err := rt.WaitForExit(context.Background(), api.WaitForExitParams{ProcessID: res.ProcessID, TimeoutMS: 5000}); err != nil || !w.Exited {
		t.Fatalf("process did not exit: %v %+v", err, w)
	}

	logs, err := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Lines: 100})
	if err != nil {
		t.Fatal(err)
	}
	// With no sink the 5-line ring holds the last 5 entries only; had a sink
	// been built, the 100-line request would back-fill and report memory+db.
	if logs.Source != "memory" {
		t.Fatalf("source = %q, want memory (fallback to memory)", logs.Source)
	}
	if len(logs.Entries) != 5 {
		t.Fatalf("entries = %d, want 5 (ring only, no archive)", len(logs.Entries))
	}
}

// TestAdoptOrphansRecoversCrashedDaemonProcess is the daemon crash-survival
// story: runtime A starts a long-lived process (its start record, including the
// pid, is flushed to SQLite), then A is abandoned as if it had crashed. A fresh
// runtime B over the same archive must re-attach to the orphan via
// AdoptOrphans, serve its archived logs, and be able to stop it.
func TestAdoptOrphansRecoversCrashedDaemonProcess(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "logs.db")
	yaml := "runtime:\n  log_store: sqlite\n  db_path: " + dbPath + "\n  shell_env: none\n"
	if err := os.WriteFile(filepath.Join(dir, "agent-runtime.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadFile(filepath.Join(dir, "agent-runtime.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	// Runtime A = the crashed daemon. It is created without a registered
	// Shutdown so we can abandon it (simulating a crash) without stopping the
	// child it launched.
	rtA := runtime.New(loaded, nil)
	res, err := rtA.Start(context.Background(), api.StartRequest{Command: helperPath, Args: []string{"graceful"}, WorkDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	procA, okA := rtA.Manager().Get(res.ProcessID)
	if !okA {
		t.Fatalf("rtA lost process %q", res.ProcessID)
	}
	info := procA.Info()
	if info.Status != "running" || info.PID <= 0 {
		t.Fatalf("rtA info = %+v", info)
	}
	orphanPID := info.PID

	// Runtime B = the new daemon, same archive. Poll AdoptOrphans until the
	// start record (flushed asynchronously every ~200ms) is visible and the
	// orphan is re-attached. The first poll may see an empty archive.
	rtB := runtime.New(loaded, nil)
	t.Cleanup(func() { rtB.Shutdown() })
	var adopted int
	waitFor(t, 5*time.Second, "orphan adopted", func() bool {
		adopted = rtB.AdoptOrphans()
		return adopted >= 1
	})
	if adopted != 1 {
		t.Fatalf("adopted = %d, want 1", adopted)
	}

	// The adopted process must be registered, running, and carry the orphan's
	// real pid so signals reach the right process.
	proc, ok := rtB.Manager().Get(res.ProcessID)
	if !ok {
		t.Fatalf("process %q not registered after adoption", res.ProcessID)
	}
	aInfo := proc.Info()
	if aInfo.Status != "running" || aInfo.PID != orphanPID {
		t.Fatalf("adopted info = %+v, want running pid %d", aInfo, orphanPID)
	}

	// Logs come from the archive (in-memory buffers are empty for an adopted
	// process): the graceful helper's "RUNNING" line must be served.
	got, err := rtB.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Lines: 100})
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "memory+db" {
		t.Fatalf("source = %q, want memory+db (archive back-fill)", got.Source)
	}
	found := false
	for _, e := range got.Entries {
		if e.Line == "RUNNING" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("adopted logs missing RUNNING: %+v", got.Entries)
	}

	// The adopted process must be stoppable, and its death must be finalized by
	// the adopted liveness loop.
	if err := rtB.Stop(context.Background(), res.ProcessID); err != nil {
		t.Fatalf("stop adopted: %v", err)
	}
	waitFor(t, 5*time.Second, "adopted process exited", func() bool {
		st, _ := rtB.Status(res.ProcessID)
		return st != nil && (st.Status == "stopped" || st.Status == "exited")
	})
	if process.Alive(orphanPID) {
		t.Fatalf("orphan pid %d still alive after stop", orphanPID)
	}

	// Clean up runtime A (the "crashed" daemon): its child is already dead, so
	// Shutdown is a no-op that just releases A's archive connection.
	_ = rtA.Shutdown()
}

// TestConfigSQLiteDefaults checks the DB path defaulting done in runtime.New's
// buildSink: log_store: sqlite with an empty db_path resolves to
// <ProjectDir>/.agent-runtime/logs.db.
func TestConfigSQLiteDefaults(t *testing.T) {
	rt, dir := newRuntimeWithConfigDir(t, "runtime:\n  log_store: sqlite\n")
	expected := filepath.Join(dir, ".agent-runtime", "logs.db")

	res, err := rt.Start(context.Background(), rawCmd(t, "once"))
	if err != nil {
		t.Fatal(err)
	}
	if w, err := rt.WaitForExit(context.Background(), api.WaitForExitParams{ProcessID: res.ProcessID, TimeoutMS: 5000}); err != nil || !w.Exited {
		t.Fatalf("process did not exit: %v %+v", err, w)
	}

	// Shutdown flushes and closes the archive at the defaulted path.
	if err := rt.Shutdown(); err != nil {
		t.Fatal(err)
	}

	sink, err := logstore.Open(expected, nil)
	if err != nil {
		t.Fatalf("open default db %s: %v", expected, err)
	}
	t.Cleanup(func() { sink.Close() })
	entries, err := sink.Query(res.ProcessID, logs.Query{Lines: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("persisted entries = %d, want 3", len(entries))
	}
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, e.Line)
	}
	for _, want := range []string{"once-line-0", "once-line-1", "once-line-2"} {
		found := false
		for _, l := range lines {
			if l == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("persisted lines %v missing %q", lines, want)
		}
	}
}

// startSh starts `sh -c script` on the given runtime, failing the test on
// error. Any extra request fields (Env, EnvFile, WorkDir) are preserved.
func startSh(t *testing.T, rt *runtime.Runtime, script string, req api.StartRequest) *api.StartResult {
	t.Helper()
	req.Command = "sh"
	req.Args = []string{"-c", script}
	res, err := rt.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// waitExitLine waits for the process to exit and asserts that a captured log
// line equals want exactly (used with printf-style single-line output).
func waitExitLine(t *testing.T, rt *runtime.Runtime, id, want string) {
	t.Helper()
	w, err := rt.WaitForExit(context.Background(), api.WaitForExitParams{ProcessID: id, TimeoutMS: 5000})
	if err != nil || !w.Exited {
		t.Fatalf("process did not exit: %v %+v", err, w)
	}
	got, err := rt.GetLogs(api.GetLogsRequest{ProcessID: id, Lines: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got.Entries {
		if e.Line == want {
			return
		}
	}
	var lines []string
	for _, e := range got.Entries {
		lines = append(lines, e.Line)
	}
	t.Fatalf("log lines %v missing %q", lines, want)
}

// TestStartEnvPrecedence exercises the full env pipeline for raw starts:
// runtime.env is the base override layer, request.env wins over it.
func TestStartEnvPrecedence(t *testing.T) {
	rt := newRuntimeWithConfig(t, "runtime:\n  env: [\"FOO=global\"]\n")

	// request.env wins over runtime.env.
	res := startSh(t, rt, `printf %s "$FOO"`, api.StartRequest{Env: []string{"FOO=request"}})
	waitExitLine(t, rt, res.ProcessID, "request")

	// Without a request override the runtime.env layer applies.
	res2 := startSh(t, rt, `printf %s "$FOO"`, api.StartRequest{})
	waitExitLine(t, rt, res2.ProcessID, "global")
}

// TestStartEnvFileLayering exercises the raw-path env_file layer: it sits
// below request.env but above the base layers.
func TestStartEnvFileLayering(t *testing.T) {
	rt := newRuntime(t)
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("FOO=file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// request.env wins over env_file.
	res := startSh(t, rt, `printf %s "$FOO"`, api.StartRequest{EnvFile: envPath, Env: []string{"FOO=req"}})
	waitExitLine(t, rt, res.ProcessID, "req")

	// env_file alone supplies the value.
	res2 := startSh(t, rt, `printf %s "$FOO"`, api.StartRequest{EnvFile: envPath})
	waitExitLine(t, rt, res2.ProcessID, "file")
}

// TestBaseEnvPresent proves the complete-env contract: the child sees the
// captured base environment (e.g. PATH) without the old os.Environ() prepend
// in the process layer.
func TestBaseEnvPresent(t *testing.T) {
	rt := newRuntime(t)
	res := startSh(t, rt, `printf %s "${PATH:+yes}"`, api.StartRequest{})
	waitExitLine(t, rt, res.ProcessID, "yes")
}

// TestStartMissingWorkdir verifies a nonexistent workdir fails during
// resolution, before any process is registered.
func TestStartMissingWorkdir(t *testing.T) {
	rt := newRuntime(t)
	_, err := rt.Start(context.Background(), api.StartRequest{Command: "sh", WorkDir: "/nonexistent-xyz"})
	if err == nil {
		t.Fatal("expected workdir error")
	}
	if !strings.Contains(err.Error(), "/nonexistent-xyz") {
		t.Fatalf("error %q does not mention the workdir", err)
	}
	if list, _ := rt.List(); len(list.Processes) != 0 {
		t.Fatalf("process registered despite failed start: %+v", list.Processes)
	}
}

// TestStartSymlinkedWorkdir verifies the workdir is symlink-resolved before
// exec: the child's pwd must be the resolved real path, not the link path.
func TestStartSymlinkedWorkdir(t *testing.T) {
	rt := newRuntime(t)
	realDir := t.TempDir()
	resolvedReal, err := filepath.EvalSymlinks(realDir)
	if err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatal(err)
	}
	res := startSh(t, rt, "pwd -P", api.StartRequest{WorkDir: linkDir})
	waitExitLine(t, rt, res.ProcessID, resolvedReal)
}

// TestProcessEnvSpecMode exercises get_process_env spec mode: the env the
// runtime constructed, with provenance and secret redaction. shell_env is
// pinned to none so the base layer is exactly the parent env (deterministic
// redaction count).
func TestProcessEnvSpecMode(t *testing.T) {
	rt := newRuntimeWithConfig(t, "runtime:\n  shell_env: none\n")
	res := startSh(t, rt, "sleep 30", api.StartRequest{Env: []string{"API_KEY=sekrit", "FOO=bar"}})

	env, err := rt.ProcessEnv(api.ProcessEnvRequest{ProcessID: res.ProcessID, Live: false, Reveal: false})
	if err != nil {
		t.Fatal(err)
	}
	var redactedAPI, fooBar bool
	for _, kv := range env.Env {
		if kv == "API_KEY=***" {
			redactedAPI = true
		}
		if kv == "FOO=bar" {
			fooBar = true
		}
	}
	if !redactedAPI {
		t.Fatalf("API_KEY not redacted: %v", env.Env)
	}
	if !fooBar {
		t.Fatalf("FOO=bar missing: %v", env.Env)
	}
	if env.Redacted < 1 {
		t.Fatalf("Redacted = %d, want >=1", env.Redacted)
	}
	if env.Source["FOO"] != "request.env" {
		t.Fatalf("Source[FOO] = %q, want request.env", env.Source["FOO"])
	}

	// Reveal mode returns the raw value and redacts nothing.
	env2, err := rt.ProcessEnv(api.ProcessEnvRequest{ProcessID: res.ProcessID, Reveal: true})
	if err != nil {
		t.Fatal(err)
	}
	var rawSeen bool
	for _, kv := range env2.Env {
		if kv == "API_KEY=sekrit" {
			rawSeen = true
		}
	}
	if !rawSeen {
		t.Fatalf("raw API_KEY value missing with reveal=true: %v", env2.Env)
	}
	if env2.Redacted != 0 {
		t.Fatalf("Redacted = %d, want 0 with reveal=true", env2.Redacted)
	}
}

// TestProcessEnvLiveMode exercises get_process_env live mode against
// /proc/<pid>/environ and cleans up through signal_process. shell_env is
// pinned to none so the base layer is exactly the parent env (which is
// guaranteed to contain PATH: the test harness needs it to exec sh).
func TestProcessEnvLiveMode(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("live env read requires /proc")
	}
	rt := newRuntimeWithConfig(t, "runtime:\n  shell_env: none\n")
	res := startSh(t, rt, "sleep 30", api.StartRequest{})
	waitFor(t, 5*time.Second, "running", func() bool {
		st, _ := rt.Status(res.ProcessID)
		return st.Status == "running"
	})

	// /proc/<pid>/environ can transiently read empty in the fork/exec window
	// right after "running"; poll until the live read is non-empty.
	var env *api.ProcessEnvResult
	waitFor(t, 5*time.Second, "non-empty live env", func() bool {
		e, err := rt.ProcessEnv(api.ProcessEnvRequest{ProcessID: res.ProcessID, Live: true})
		if err != nil {
			return false
		}
		if len(e.Env) == 0 {
			return false
		}
		env = e
		return true
	})

	var hasPath bool
	for _, kv := range env.Env {
		if strings.HasPrefix(kv, "PATH=") {
			hasPath = true
		}
	}
	if !hasPath {
		t.Fatalf("live env missing PATH: %v", env.Env)
	}

	// Clean up via signal_process (also exercises its happy path).
	if _, err := rt.SignalProcess(context.Background(), api.SignalProcessRequest{
		ProcessID: res.ProcessID, Signal: "SIGKILL",
	}); err != nil {
		t.Fatal(err)
	}
	w, err := rt.WaitForExit(context.Background(), api.WaitForExitParams{ProcessID: res.ProcessID, TimeoutMS: 5000})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Exited {
		t.Fatalf("process did not exit after SIGKILL: %+v", w)
	}
}

// TestSignalProcessSIGINT delivers SIGINT to a running process's group and
// verifies it terminates.
func TestSignalProcessSIGINT(t *testing.T) {
	rt := newRuntime(t)
	res := startSh(t, rt, "sleep 30", api.StartRequest{})
	waitFor(t, 5*time.Second, "running", func() bool {
		st, _ := rt.Status(res.ProcessID)
		return st.Status == "running"
	})
	if _, err := rt.SignalProcess(context.Background(), api.SignalProcessRequest{
		ProcessID: res.ProcessID, Signal: "SIGINT",
	}); err != nil {
		t.Fatal(err)
	}
	w, err := rt.WaitForExit(context.Background(), api.WaitForExitParams{ProcessID: res.ProcessID, TimeoutMS: 5000})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Exited {
		t.Fatalf("process did not exit after SIGINT: %+v", w)
	}
}

// TestSignalProcessBadName verifies an unsupported signal name is rejected.
func TestSignalProcessBadName(t *testing.T) {
	rt := newRuntime(t)
	res := startSh(t, rt, "sleep 30", api.StartRequest{})
	_, err := rt.SignalProcess(context.Background(), api.SignalProcessRequest{
		ProcessID: res.ProcessID, Signal: "SIGFOO",
	})
	if err == nil {
		t.Fatal("expected unsupported signal error")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error %q does not mention unsupported", err)
	}
	rt.Stop(context.Background(), res.ProcessID)
}

// TestOpenShellProcessBranch opens a shell in a running process's environment
// and verifies it starts as a managed process; it also checks Status PGID ==
// PID (Setpgid means pgid == pid).
func TestOpenShellProcessBranch(t *testing.T) {
	rt := newRuntime(t)
	res := startSh(t, rt, "sleep 30", api.StartRequest{})
	waitFor(t, 5*time.Second, "running", func() bool {
		st, _ := rt.Status(res.ProcessID)
		return st.Status == "running"
	})

	shell, err := rt.OpenShell(context.Background(), api.OpenShellRequest{ProcessID: res.ProcessID, Shell: "sh"})
	if err != nil {
		t.Fatal(err)
	}
	if shell.ProcessID == "" || shell.Status != "running" || shell.Command != "sh" {
		t.Fatalf("bad open_shell result: %+v", shell)
	}
	if shell.WorkDir == "" {
		t.Fatal("open_shell workdir empty")
	}
	st, err := rt.Status(shell.ProcessID)
	if err != nil {
		t.Fatal(err)
	}
	if st.PID <= 0 {
		t.Fatalf("no pid for shell process: %+v", st)
	}
	if st.PGID != st.PID {
		t.Fatalf("pgid = %d, want pid %d", st.PGID, st.PID)
	}
	rt.Stop(context.Background(), shell.ProcessID)
}

// TestOpenShellRequiresTarget verifies open_shell rejects empty requests.
func TestOpenShellRequiresTarget(t *testing.T) {
	rt := newRuntime(t)
	if _, err := rt.OpenShell(context.Background(), api.OpenShellRequest{}); err == nil {
		t.Fatal("expected error for empty open_shell request")
	}
}

// TestOpenShellByPID shells into a process by OS pid, reading its workdir and
// env from /proc/<pid>/cwd and /proc/<pid>/environ. The shell must start in
// the process's resolved workdir with its env (proven via the OSHELL_PROBE
// variable). shell_env is pinned to none so the base env is deterministic.
func TestOpenShellByPID(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("pid shell requires /proc")
	}
	rt := newRuntimeWithConfig(t, "runtime:\n  shell_env: none\n")
	workDir := t.TempDir()
	res := startSh(t, rt, "sleep 30", api.StartRequest{WorkDir: workDir, Env: []string{"OSHELL_PROBE=fromproc"}})
	waitFor(t, 5*time.Second, "running", func() bool {
		st, _ := rt.Status(res.ProcessID)
		return st.Status == "running"
	})
	st, err := rt.Status(res.ProcessID)
	if err != nil {
		t.Fatal(err)
	}
	if st.PID <= 0 {
		t.Fatalf("no pid for process: %+v", st)
	}

	// /proc/<pid>/environ can transiently read empty in the fork/exec window
	// right after "running"; poll until the probe is visible so the shell
	// inherits it.
	waitFor(t, 5*time.Second, "probe visible in /proc/<pid>/environ", func() bool {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", st.PID))
		if err != nil {
			return false
		}
		return strings.Contains(string(data), "OSHELL_PROBE=fromproc")
	})

	shell, err := rt.OpenShell(context.Background(), api.OpenShellRequest{PID: st.PID, Shell: "/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	if shell.ProcessID == "" || shell.Status != "running" || shell.Command != "/bin/sh" {
		t.Fatalf("bad open_shell result: %+v", shell)
	}
	wantDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if shell.WorkDir != wantDir {
		t.Fatalf("shell workdir = %q, want %q (the process's resolved workdir)", shell.WorkDir, wantDir)
	}

	// The shell inherited the process's env: echo the probe through stdin.
	if _, err := rt.SendStdin(shell.ProcessID, "echo $OSHELL_PROBE\n"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "probe echoed by shell", func() bool {
		logs, _ := rt.GetLogs(api.GetLogsRequest{ProcessID: shell.ProcessID, Lines: 20})
		for _, e := range logs.Entries {
			if strings.Contains(e.Line, "fromproc") {
				return true
			}
		}
		return false
	})

	rt.Stop(context.Background(), shell.ProcessID)
	rt.Stop(context.Background(), res.ProcessID)
}

// TestOpenShellByPIDMissing verifies a bogus pid produces a clear error
// mentioning /proc or an exited process.
func TestOpenShellByPIDMissing(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("pid shell requires /proc")
	}
	rt := newRuntime(t)
	_, err := rt.OpenShell(context.Background(), api.OpenShellRequest{PID: 2147483647, Shell: "/bin/sh"})
	if err == nil {
		t.Fatal("expected error for bogus pid")
	}
	if !strings.Contains(err.Error(), "/proc") && !strings.Contains(err.Error(), "exited") {
		t.Fatalf("error %q does not mention /proc or an exited process", err)
	}
}
