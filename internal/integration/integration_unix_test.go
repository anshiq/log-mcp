//go:build !windows

package integration_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"agent-runtime/pkg/api"
)

// parsePID extracts an integer from a log line of the form "label=123".
func parsePID(line, label string) int {
	if !strings.HasPrefix(line, label) {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimPrefix(line, label))
	if err != nil {
		return 0
	}
	return n
}

// TestProcessTreeKillNoOrphans starts a 3-level process tree (middle sh ->
// grandchild sh -> sleep), captures the grandchild's pid from its own output,
// then verifies stop_process's process-group kill reaps both the middle and
// the grandchild. This mirrors the mycli -> uv -> python tree: a group kill
// must not leave orphans behind.
func TestProcessTreeKillNoOrphans(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}
	rt := newRuntimeFromFixture(t, "basic")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// The middle sh stays alive (it waits on the background job); the
	// grandchild prints its own pid, then sleeps. Both pids land in the
	// captured stdout, which the harness reads via get_logs.
	const treeScript = `echo "middle=$$"; sh -c 'echo "grandchild=$$"; sleep 30' & wait`
	res, err := rt.Start(ctx, api.StartRequest{Command: "sh", Args: []string{"-c", treeScript}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupProcess(rt, res.ProcessID) })

	// Poll the log buffer until both pids have been printed.
	var middle, grandchild int
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		logs, err := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Lines: 20})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range logs.Entries {
			if m := parsePID(e.Line, "middle="); m > 0 {
				middle = m
			}
			if g := parsePID(e.Line, "grandchild="); g > 0 {
				grandchild = g
			}
		}
		if middle > 0 && grandchild > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if middle <= 0 || grandchild <= 0 {
		t.Fatalf("tree pids not captured (middle=%d grandchild=%d)", middle, grandchild)
	}

	if err := rt.Stop(ctx, res.ProcessID); err != nil {
		t.Fatal(err)
	}

	// Poll kill(pid, 0) until the grandchild reports "no such process". If the
	// assertion is about to fail, SIGKILL the survivors so nothing leaks.
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(grandchild, 0), syscall.ESRCH) {
			if err := syscall.Kill(middle, 0); !errors.Is(err, syscall.ESRCH) {
				syscall.Kill(middle, syscall.SIGKILL)
				t.Fatalf("middle pid %d survived stop_process", middle)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	syscall.Kill(grandchild, syscall.SIGKILL)
	syscall.Kill(middle, syscall.SIGKILL)
	t.Fatalf("grandchild pid %d still alive after stop_process (leaked)", grandchild)
}

// TestSignalProcessSIGINTGraceful delivers SIGINT to a process whose trap
// prints GOTINT and exits 0: the signal must reach the group, the trap must
// run, and the exit code must be 0.
func TestSignalProcessSIGINTGraceful(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}
	rt := newRuntimeFromFixture(t, "basic")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// READY is printed only after the INT trap is armed, so waiting for it
	// guarantees the signal cannot land before the trap exists (status flips to
	// "running" at exec time, which races the shell's startup).
	const script = `trap "echo GOTINT; exit 0" INT; echo READY; while true; do sleep 1; done`
	res, err := rt.Start(ctx, api.StartRequest{Command: "sh", Args: []string{"-c", script}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupProcess(rt, res.ProcessID) })
	wready, err := rt.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: res.ProcessID, Contains: "READY", TimeoutMS: 10000})
	if err != nil || !wready.Matched {
		t.Fatalf("trap not armed (no READY): %v %+v", err, wready)
	}

	if _, err := rt.SignalProcess(ctx, api.SignalProcessRequest{ProcessID: res.ProcessID, Signal: "SIGINT"}); err != nil {
		t.Fatal(err)
	}

	wl, err := rt.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: res.ProcessID, Contains: "GOTINT", TimeoutMS: 5000})
	if err != nil || !wl.Matched {
		t.Fatalf("trap output GOTINT not seen after SIGINT: %v %+v", err, wl)
	}
	we, err := rt.WaitForExit(ctx, api.WaitForExitParams{ProcessID: res.ProcessID, TimeoutMS: 5000})
	if err != nil || !we.Exited {
		t.Fatalf("process did not exit after SIGINT: %v %+v", err, we)
	}
	if we.ExitCode == nil || *we.ExitCode != 0 {
		t.Fatalf("exit code = %v, want 0", we.ExitCode)
	}
}

// TestOpenShellSeesMergedEnv starts a process with request env, opens a shell
// inside that process's environment and verifies the shell sees the merged
// value: env set at start time must be visible to open_shell.
func TestOpenShellSeesMergedEnv(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}
	rt := newRuntimeFromFixture(t, "basic")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := rt.Start(ctx, api.StartRequest{
		Command: "sh", Args: []string{"-c", "sleep 30"},
		Env: []string{"OPEN_SHELL_PROBE=hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupProcess(rt, res.ProcessID) })
	waitRunning(t, rt, res.ProcessID)

	shell, err := rt.OpenShell(ctx, api.OpenShellRequest{ProcessID: res.ProcessID, Shell: "sh"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupProcess(rt, shell.ProcessID) })
	waitRunning(t, rt, shell.ProcessID)

	if _, err := rt.SendStdin(shell.ProcessID, "echo $OPEN_SHELL_PROBE\n"); err != nil {
		t.Fatal(err)
	}
	wl, err := rt.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: shell.ProcessID, Contains: "hello", TimeoutMS: 5000})
	if err != nil || !wl.Matched {
		t.Fatalf("open shell did not see merged request env: %v %+v", err, wl)
	}
}
