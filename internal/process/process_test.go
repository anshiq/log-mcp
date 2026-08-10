package process_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

// TestShutdownConcurrent exercises R3: Shutdown must signal all processes at
// once so total wall-clock is ~the per-process grace period, not N x grace.
// Six SIGTERM-ignoring processes each take a 300ms grace before SIGKILL; a
// sequential shutdown would take ~1.8s and blow the 1s budget.
func TestShutdownConcurrent(t *testing.T) {
	m := process.New(context.Background(), process.Options{DefaultGrace: 300 * time.Millisecond})
	const n = 6
	for i := 0; i < n; i++ {
		if _, err := m.Start(context.Background(), helperCommand(t, "ignore-term"), "test", 0); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, 5*time.Second, "all running", func() bool {
		running := 0
		for _, p := range m.List() {
			if p.Info().Status == process.StatusRunning {
				running++
			}
		}
		return running == n
	})

	timeout := time.Second
	start := time.Now()
	if err := m.Shutdown(timeout); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > timeout {
		t.Fatalf("shutdown took %v; concurrent shutdown must complete within the %v timeout (N x grace would take ~%v)", elapsed, timeout, n*300*time.Millisecond)
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

// TestRemoveExitedProcess exercises R4: removing a terminal process deletes it
// from the registry and frees its log buffers.
func TestRemoveExitedProcess(t *testing.T) {
	m := newManager(t)
	proc, err := m.Start(context.Background(), helperCommand(t, "once"), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	<-proc.Done()
	if err := m.Remove(proc.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Get(proc.ID); ok {
		t.Fatal("removed process still in registry")
	}
	if m.Logs().GetOrNil(proc.ID) != nil {
		t.Fatal("removed process log buffers still present")
	}
	if err := m.Remove(proc.ID, false); err != process.ErrNotFound {
		t.Fatalf("second remove error = %v, want ErrNotFound", err)
	}
}

// TestRemoveRunningRefused exercises R4: a running process cannot be removed
// unless force is set.
func TestRemoveRunningRefused(t *testing.T) {
	m := newManager(t)
	proc, err := m.Start(context.Background(), helperCommand(t, "ignore-term"), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "running", func() bool { return proc.Info().Status == process.StatusRunning })
	if err := m.Remove(proc.ID, false); err == nil {
		t.Fatal("expected error removing a running process without force")
	}
	if _, ok := m.Get(proc.ID); !ok {
		t.Fatal("refused removal must leave the process in the registry")
	}
	if err := m.Stop(context.Background(), proc.ID); err != nil {
		t.Fatal(err)
	}
}

// TestRemoveRunningForce exercises R4: force=true stops the process first, then
// removes it.
func TestRemoveRunningForce(t *testing.T) {
	m := newManager(t)
	proc, err := m.Start(context.Background(), helperCommand(t, "ignore-term"), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "running", func() bool { return proc.Info().Status == process.StatusRunning })
	if err := m.Remove(proc.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Get(proc.ID); ok {
		t.Fatal("force-removed process still in registry")
	}
	if m.Logs().GetOrNil(proc.ID) != nil {
		t.Fatal("force-removed process log buffers still present")
	}
}

// TestExitedProcessEviction exercises R4: terminal processes beyond the cap are
// evicted oldest-first while running processes are untouched.
func TestExitedProcessEviction(t *testing.T) {
	m := process.New(context.Background(), process.Options{MaxExitedProcesses: 2})
	t.Cleanup(func() { m.Shutdown(5 * time.Second) })
	const n = 5
	for i := 0; i < n; i++ {
		proc, err := m.Start(context.Background(), helperCommand(t, "once"), "test", 0)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-proc.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("process did not exit")
		}
	}
	// Eviction runs synchronously in waitLoop before done closes; the last
	// Done implies the registry has already been trimmed.
	if got := len(m.List()); got > 2 {
		t.Fatalf("registry has %d terminal processes, want <= 2", got)
	}
	for _, p := range m.List() {
		switch p.Info().Status {
		case process.StatusExited, process.StatusStopped, process.StatusFailed:
		default:
			t.Fatalf("eviction must keep only terminal processes, got %q", p.Info().Status)
		}
	}
}

// TestSendStdinConcurrentWithExitNoRace exercises B2: SendStdin reads the
// instance status without holding proc.mu while waitLoop writes it under the
// lock. A tight caller loop racing a short-lived process's exit must not trip
// the race detector.
func TestSendStdinConcurrentWithExitNoRace(t *testing.T) {
	m := newManager(t)
	for iter := 0; iter < 10; iter++ {
		proc, err := m.Start(context.Background(), helperCommand(t, "stderr-exit"), "test", 0)
		if err != nil {
			t.Fatal(err)
		}
		stop := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
						// Errors (dead process / stdin closed) are expected and fine.
						_ = proc.SendStdin("data\n")
					}
				}
			}()
		}
		select {
		case <-proc.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("process did not exit")
		}
		time.Sleep(20 * time.Millisecond) // widen the transition window
		close(stop)
		wg.Wait()
	}
}

// TestStartConcurrentInfoNoRace exercises B3: startInstance publishes
// proc.inst and then mutates pid/started (success) or exitCode/exited
// (failure) without proc.mu while Info() reads them under the lock. Polling
// Info() during starts of both a failing and a short-lived command must not
// trip the race detector.
func TestStartConcurrentInfoNoRace(t *testing.T) {
	m := newManager(t)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				for _, p := range m.List() {
					_ = p.Info()
				}
			}
		}
	}()

	for i := 0; i < 30; i++ {
		// Failure path: cmd.Start() fails after inst is published.
		spec := process.StartSpec{Command: "/nonexistent/binary-that-does-not-exist", WorkDir: t.TempDir()}
		m.Start(context.Background(), spec, "test", 0)
	}
	for i := 0; i < 30; i++ {
		// Success path: a short-lived command keeps inst alive while Info polls.
		proc, err := m.Start(context.Background(), helperCommand(t, "once"), "test", 0)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-proc.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("process did not exit")
		}
	}
	close(stop)
	wg.Wait()
}

// TestFinalLinesFlushedBeforeDone exercises B4: waitLoop previously closed the
// done channel as soon as cmd.Wait() returned, potentially before the
// stdout/stderr readers had flushed their final buffered lines. A process that
// writes many lines and exits immediately must have ALL of them visible once
// Done fires. Loop many times because the loss is timing-dependent.
func TestFinalLinesFlushedBeforeDone(t *testing.T) {
	m := newManager(t)
	const lines = 50
	for i := 0; i < 150; i++ {
		proc, err := m.Start(context.Background(), helperCommand(t, "burst", strconv.Itoa(lines)), "test", 0)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-proc.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("process did not exit")
		}
		got := proc.Logs.Count(logs.StreamStdout)
		if got != lines {
			t.Fatalf("iteration %d: stdout lines = %d, want %d (final lines lost before done)", i, got, lines)
		}
	}
}

func TestSignalByName(t *testing.T) {
	cases := []struct {
		name string
		want syscall.Signal
	}{
		{"SIGINT", syscall.SIGINT},
		{"INT", syscall.SIGINT},
		{"sigterm", syscall.SIGTERM},
		{"TERM", syscall.SIGTERM},
		{"SIGHUP", syscall.SIGHUP},
		{"QUIT", syscall.SIGQUIT},
		{"SIGUSR1", syscall.SIGUSR1},
		{"usr2", syscall.SIGUSR2},
		{"SIGKILL", syscall.SIGKILL},
		{"kill", syscall.SIGKILL},
	}
	for _, c := range cases {
		got, err := process.SignalByName(c.name)
		if err != nil {
			t.Fatalf("SignalByName(%q): %v", c.name, err)
		}
		if got != c.want {
			t.Fatalf("SignalByName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
	bad := []string{"", "SIG", "SIGFOO", "SEGV", "ALRM", "sigstop"}
	for _, name := range bad {
		if _, err := process.SignalByName(name); err == nil {
			t.Fatalf("SignalByName(%q): expected error", name)
		}
	}
}

func TestSignalUnknownProcess(t *testing.T) {
	m := newManager(t)
	if err := m.Signal(context.Background(), "does-not-exist", syscall.SIGTERM); err != process.ErrNotFound {
		t.Fatalf("Signal unknown id error = %v, want ErrNotFound", err)
	}
}

func TestSignalExitedProcess(t *testing.T) {
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
	err = m.Signal(context.Background(), proc.ID, syscall.SIGTERM)
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("Signal on exited process error = %v, want a 'not running' error", err)
	}
}

// TestSignalKillTerminates verifies that a SIGKILL delivered via Signal kills
// a running (even SIGTERM-ignoring) helper without Stop's escalation machinery.
func TestSignalKillTerminates(t *testing.T) {
	m := newManager(t)
	proc, err := m.Start(context.Background(), helperCommand(t, "ignore-term"), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "running", func() bool { return proc.Info().Status == process.StatusRunning })

	if err := m.Signal(context.Background(), proc.ID, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	select {
	case <-proc.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not die after SIGKILL via Signal")
	}
	info := proc.Info()
	if info.Status != process.StatusExited {
		t.Fatalf("status = %q, want exited", info.Status)
	}
	if info.ExitCode == nil || *info.ExitCode != -1 {
		t.Fatalf("killed exit code = %v, want -1 (signal)", info.ExitCode)
	}
}
