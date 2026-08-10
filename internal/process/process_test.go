package process_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"agent-runtime/internal/logs"
	"agent-runtime/internal/process"
)

var helperPath string

// TestMain compiles the standalone helper binary used by all process tests.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "agent-runtime-helper-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	helperPath = filepath.Join(dir, "helper")
	cmd := exec.Command("go", "build", "-o", helperPath, "./testdata/helper")
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

func helperCommand(t *testing.T, behaviour string, args ...string) process.StartSpec {
	t.Helper()
	cmd := []string{helperPath, behaviour}
	cmd = append(cmd, args...)
	return process.StartSpec{
		Command: cmd[0],
		Args:    cmd[1:],
		WorkDir: t.TempDir(),
	}
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

func allStdout(p *process.ManagedProcess) []logs.Entry {
	return p.Logs.Query(logs.Query{Stream: logs.FilterStdout}).Entries
}

func newManager(t *testing.T) *process.Manager {
	t.Helper()
	m := process.New(context.Background(), process.Options{})
	t.Cleanup(func() { m.Shutdown(5 * time.Second) })
	return m
}

func TestStartReturnsImmediately(t *testing.T) {
	m := newManager(t)
	start := time.Now()
	proc, err := m.Start(context.Background(), helperCommand(t, "ignore-term"), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Start took %v; must return immediately", elapsed)
	}
	info := proc.Info()
	if info.Status != process.StatusRunning {
		t.Fatalf("status = %q, want running", info.Status)
	}
	if info.PID <= 0 {
		t.Fatalf("pid = %d", info.PID)
	}
	if !strings.HasPrefix(info.ID, "proc_") || !strings.HasPrefix(info.InstanceID, "run_") {
		t.Fatalf("bad ids: %s / %s", info.ID, info.InstanceID)
	}
	if err := m.Stop(context.Background(), info.ID); err != nil {
		t.Fatal(err)
	}
}

func TestStdoutCapture(t *testing.T) {
	m := newManager(t)
	proc, err := m.Start(context.Background(), helperCommand(t, "print", "5"), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "6 stdout lines", func() bool { return proc.Logs.Count(logs.StreamStdout) >= 6 })
	entries := allStdout(proc)
	if len(entries) < 6 {
		t.Fatalf("stdout entries = %d, want >= 6", len(entries))
	}
	if entries[0].Line != "out-line-0" || entries[5].Line != "READY" {
		t.Fatalf("unexpected lines: %q %q", entries[0].Line, entries[5].Line)
	}
	if err := m.Stop(context.Background(), proc.ID); err != nil {
		t.Fatal(err)
	}
}

func TestStderrCaptureAndExitCode(t *testing.T) {
	m := newManager(t)
	proc, err := m.Start(context.Background(), helperCommand(t, "stderr-exit"), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-proc.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit")
	}
	info := proc.Info()
	if info.Status != process.StatusExited {
		t.Fatalf("status = %q, want exited", info.Status)
	}
	if info.ExitCode == nil || *info.ExitCode != 1 {
		t.Fatalf("exit code = %v, want 1", info.ExitCode)
	}
	res := proc.Logs.Query(logs.Query{Stream: logs.FilterStderr})
	if len(res.Entries) != 2 || res.Entries[0].Line != "boom-on-stderr" {
		t.Fatalf("stderr entries = %+v", res.Entries)
	}
	if proc.Logs.Count(logs.StreamStdout) != 0 {
		t.Fatalf("stdout should be empty, has %d", proc.Logs.Count(logs.StreamStdout))
	}
}

func TestFailedCommand(t *testing.T) {
	m := newManager(t)
	spec := process.StartSpec{Command: "/nonexistent/binary-that-does-not-exist", WorkDir: t.TempDir()}
	if _, err := m.Start(context.Background(), spec, "test", 0); err == nil {
		t.Fatal("expected start error")
	}
	if len(m.List()) != 0 {
		t.Fatal("failed process must be removed from registry")
	}
}

func TestInvalidWorkdir(t *testing.T) {
	m := newManager(t)
	spec := process.StartSpec{Command: "go", WorkDir: "/definitely/not/a/real/dir/xyz"}
	if _, err := m.Start(context.Background(), spec, "test", 0); err == nil {
		t.Fatal("expected workdir validation error")
	}
}

func TestEmptyCommand(t *testing.T) {
	m := newManager(t)
	if _, err := m.Start(context.Background(), process.StartSpec{WorkDir: t.TempDir()}, "test", 0); err == nil {
		t.Fatal("expected empty-command error")
	}
}

func TestStopGraceful(t *testing.T) {
	m := newManager(t)
	proc, err := m.Start(context.Background(), helperCommand(t, "graceful"), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "running", func() bool { return proc.Info().Status == process.StatusRunning })
	// Wait for the helper to register its SIGTERM handler (it prints RUNNING
	// after signal.Notify), otherwise Stop may TERM the process too early.
	waitFor(t, 5*time.Second, "RUNNING line", func() bool {
		for _, e := range allStdout(proc) {
			if e.Line == "RUNNING" {
				return true
			}
		}
		return false
	})

	if err := m.Stop(context.Background(), proc.ID); err != nil {
		t.Fatal(err)
	}
	info := proc.Info()
	if info.Status != process.StatusStopped {
		t.Fatalf("status = %q, want stopped", info.Status)
	}
	if info.ExitCode == nil || *info.ExitCode != 0 {
		t.Fatalf("exit code = %v, want 0", info.ExitCode)
	}
	waitFor(t, 5*time.Second, "signal line", func() bool {
		for _, e := range allStdout(proc) {
			if strings.HasPrefix(e.Line, "recv-") {
				return true
			}
		}
		return false
	})
}

func TestStopEscalatesToKill(t *testing.T) {
	m := process.New(context.Background(), process.Options{DefaultGrace: 300 * time.Millisecond})
	proc, err := m.Start(context.Background(), helperCommand(t, "ignore-term"), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "running", func() bool { return proc.Info().Status == process.StatusRunning })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := time.Now()
	if err := m.Stop(ctx, proc.ID); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 12*time.Second {
		t.Fatalf("stop took %v, expected bounded escalation", elapsed)
	}
	info := proc.Info()
	if info.Status != process.StatusStopped {
		t.Fatalf("status = %q, want stopped", info.Status)
	}
	if info.ExitCode == nil || *info.ExitCode != -1 {
		t.Fatalf("killed exit code = %v, want -1 (signal)", info.ExitCode)
	}
}

func TestGroupKillTerminatesGrandchildren(t *testing.T) {
	if os.Getenv("CI") == "true" {
		t.Skip("process-group semantics vary in CI sandboxes")
	}
	m := process.New(context.Background(), process.Options{DefaultGrace: 200 * time.Millisecond})
	proc, err := m.Start(context.Background(), helperCommand(t, "spawn"), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	var childPid int
	waitFor(t, 5*time.Second, "CHILD= line", func() bool {
		for _, e := range allStdout(proc) {
			if strings.HasPrefix(e.Line, "CHILD=") {
				childPid, _ = strconv.Atoi(strings.TrimPrefix(e.Line, "CHILD="))
				return true
			}
		}
		return false
	})
	if childPid <= 0 {
		t.Fatal("no grandchild pid captured")
	}

	if err := m.Stop(context.Background(), proc.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "grandchild reaped", func() bool {
		err := syscall.Kill(childPid, 0)
		return err == syscall.ESRCH
	})
}

func TestRestartPreservesIdentity(t *testing.T) {
	m := newManager(t)
	proc, err := m.Start(context.Background(), helperCommand(t, "print", "3"), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	firstID := proc.InstanceID()
	waitFor(t, 5*time.Second, "first run logs", func() bool {
		return proc.Logs.Count(logs.StreamStdout) >= 4
	})

	if err := m.Restart(context.Background(), proc.ID); err != nil {
		t.Fatal(err)
	}
	secondID := proc.InstanceID()
	if secondID == firstID {
		t.Fatalf("instance id did not change: %s", secondID)
	}
	waitFor(t, 5*time.Second, "second run logs", func() bool {
		return proc.Logs.Count(logs.StreamStdout) >= 8
	})
	if info := proc.Info(); info.Restarts != 1 {
		t.Fatalf("restarts = %d, want 1", info.Restarts)
	}
	if err := m.Stop(context.Background(), proc.ID); err != nil {
		t.Fatal(err)
	}
}

func TestStdin(t *testing.T) {
	m := newManager(t)
	proc, err := m.Start(context.Background(), helperCommand(t, "stdin-echo"), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "running", func() bool { return proc.Info().Status == process.StatusRunning })
	if err := proc.SendStdin("hello world\n"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "echoed input", func() bool {
		for _, e := range allStdout(proc) {
			if e.Line == "got: hello world" {
				return true
			}
		}
		return false
	})
	proc.CloseStdin()
	select {
	case <-proc.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("child did not exit after stdin closed")
	}
}

func TestStdinToDeadProcess(t *testing.T) {
	m := newManager(t)
	proc, err := m.Start(context.Background(), helperCommand(t, "stderr-exit"), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-proc.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("did not exit")
	}
	if err := proc.SendStdin("x\n"); err == nil {
		t.Fatal("expected error writing stdin to dead process")
	}
}

func TestConcurrentProcessesAreIndependent(t *testing.T) {
	m := newManager(t)
	procs := make([]*process.ManagedProcess, 4)
	for i := range procs {
		p, err := m.Start(context.Background(), helperCommand(t, "print", "2"), "test", 0)
		if err != nil {
			t.Fatal(err)
		}
		procs[i] = p
	}
	waitFor(t, 5*time.Second, "all running", func() bool {
		for _, p := range procs {
			if p.Info().Status != process.StatusRunning {
				return false
			}
		}
		return true
	})
	if err := m.Stop(context.Background(), procs[0].ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	for i := 1; i < len(procs); i++ {
		if procs[i].Info().Status != process.StatusRunning {
			t.Fatalf("process %d affected by stopping process 0", i)
		}
	}
	for i := 1; i < len(procs); i++ {
		m.Stop(context.Background(), procs[i].ID)
	}
}

func TestEventsEmitted(t *testing.T) {
	m := newManager(t)
	ch, unsub := m.Events().Subscribe()
	defer unsub()
	proc, err := m.Start(context.Background(), helperCommand(t, "stderr-exit"), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	gotStarted, gotExited := false, false
	deadline := time.After(5 * time.Second)
	for !gotStarted || !gotExited {
		select {
		case e := <-ch:
			if e.ProcessID != proc.ID {
				continue
			}
			switch e.Type {
			case "process.started":
				gotStarted = true
			case "process.exited":
				gotExited = true
				if e.Payload.(map[string]any)["exit_code"] != 1 {
					t.Fatal("bad exit code in event")
				}
			}
		case <-deadline:
			t.Fatalf("missing events started=%v exited=%v", gotStarted, gotExited)
		}
	}
}

func TestShutdownStopsEverything(t *testing.T) {
	m := newManager(t)
	for i := 0; i < 3; i++ {
		if _, err := m.Start(context.Background(), helperCommand(t, "graceful"), "test", 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Shutdown(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	for _, p := range m.List() {
		if p.Info().Status != process.StatusStopped {
			t.Fatalf("process %s status = %q after shutdown", p.ID, p.Info().Status)
		}
	}
}

func TestPartialLineFragments(t *testing.T) {
	m := newManager(t)
	proc, err := m.Start(context.Background(), helperCommand(t, "print", "2"), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "log lines", func() bool { return proc.Logs.Count(logs.StreamStdout) >= 3 })
	for _, e := range allStdout(proc) {
		if strings.ContainsAny(e.Line, "\n") {
			t.Fatalf("line contains newline: %q", e.Line)
		}
	}
	m.Stop(context.Background(), proc.ID)
}

func TestListAfterExit(t *testing.T) {
	m := newManager(t)
	proc, err := m.Start(context.Background(), helperCommand(t, "stderr-exit"), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	<-proc.Done()
	found := false
	for _, p := range m.List() {
		if p.ID == proc.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("exited process missing from list")
	}
}
