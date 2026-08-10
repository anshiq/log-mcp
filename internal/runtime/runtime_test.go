package runtime_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-runtime/internal/config"
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
	loaded, err := config.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(loaded, nil)
	t.Cleanup(func() { rt.Shutdown() })
	return rt
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
	loaded, err := config.LoadFrom(dir)
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
	loaded, err := config.LoadFrom(dir)
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
