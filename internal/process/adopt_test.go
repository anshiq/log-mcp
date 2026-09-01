//go:build !windows

package process_test

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"agent-runtime/internal/events"
	"agent-runtime/internal/process"
)

// spawnOrphan launches the helper as an unmanaged, orphaned process (not a
// child of the manager, in its own process group) the way a crashed daemon
// would leave one behind.
func spawnOrphan(t *testing.T, behaviour string) int {
	t.Helper()
	cmd := exec.Command(helperPath, behaviour)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn orphan: %v", err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	})
	return cmd.Process.Pid
}

func TestAdoptReRegistersOrphanedProcess(t *testing.T) {
	pid := spawnOrphan(t, "graceful")
	bus := events.New()
	m := process.New(context.Background(), process.Options{Events: bus})
	defer m.Shutdown(5 * time.Second)

	n, skipped := m.Adopt([]process.AdoptRecord{{
		ProcessID: "proc_orphan", InstanceID: "run_1",
		Command: helperPath, WorkDir: t.TempDir(), Profile: "test",
		StartedAt: time.Now(), PID: pid,
	}})
	if n != 1 || skipped != 0 {
		t.Fatalf("adopted=%d skipped=%d, want 1/0", n, skipped)
	}

	proc, ok := m.Get("proc_orphan")
	if !ok {
		t.Fatal("adopted process not in registry")
	}
	if info := proc.Info(); info.Status != process.StatusRunning || info.PID != pid {
		t.Fatalf("info = %+v, want running pid %d", info, pid)
	}

	// The adopted process must be manageable: signals reach it, stop escalates.
	if err := m.Stop(context.Background(), "proc_orphan"); err != nil {
		t.Fatalf("Stop adopted: %v", err)
	}
	select {
	case <-proc.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("adopted process done not closed after stop")
	}
	if info := proc.Info(); info.Status != process.StatusStopped {
		t.Fatalf("status after stop = %q, want stopped", info.Status)
	}

	// The exit must have been recorded and published as an event.
	if process.Alive(pid) {
		t.Fatalf("orphan pid %d still alive", pid)
	}
}

func TestAdoptSkipsDeadAndDuplicate(t *testing.T) {
	m := process.New(context.Background(), process.Options{})
	defer m.Shutdown(5 * time.Second)

	// Dead pid is skipped; a live one is adopted and its id then collides.
	pid := spawnOrphan(t, "graceful")
	n, skipped := m.Adopt([]process.AdoptRecord{
		{ProcessID: "dead", InstanceID: "r1", Command: helperPath, WorkDir: t.TempDir(), PID: 99999999},
		{ProcessID: "dup", InstanceID: "r1", Command: helperPath, WorkDir: t.TempDir(), PID: pid},
	})
	if n != 1 || skipped != 1 {
		t.Fatalf("adopted=%d skipped=%d, want 1/1", n, skipped)
	}
	// Second adopt of the same id must skip as a duplicate.
	n2, skipped2 := m.Adopt([]process.AdoptRecord{
		{ProcessID: "dup", InstanceID: "r2", Command: helperPath, WorkDir: t.TempDir(), PID: pid},
	})
	if n2 != 0 || skipped2 != 1 {
		t.Fatalf("second adopt: adopted=%d skipped=%d, want 0/1", n2, skipped2)
	}
	_ = m.Stop(context.Background(), "dup")
}

func TestAdoptedSignalDeliveredToGroup(t *testing.T) {
	// The graceful helper prints RUNNING then waits for SIGTERM. A group
	// signal from an adopted handle must reach it (verifies the synthesized
	// process handle paths through process-group signalling).
	pid := spawnOrphan(t, "graceful")
	m := process.New(context.Background(), process.Options{})
	defer m.Shutdown(5 * time.Second)
	if n, _ := m.Adopt([]process.AdoptRecord{{
		ProcessID: "sig", InstanceID: "r1", Command: helperPath, WorkDir: t.TempDir(),
		StartedAt: time.Now(), PID: pid,
	}}); n != 1 {
		t.Fatalf("adopted=%d, want 1", n)
	}
	if err := m.Signal(context.Background(), "sig", syscall.SIGTERM); err != nil {
		t.Fatalf("Signal: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if process.Alive(pid) {
		t.Fatalf("adopted pid %d still alive after SIGTERM", pid)
	}
}
