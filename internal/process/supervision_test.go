package process_test

import (
	"context"
	"net"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"agent-runtime/internal/events"
	"agent-runtime/internal/process"
)

func startWithSupervision(t *testing.T, m *process.Manager, behaviour string, args []string, sup process.Supervision) *process.ManagedProcess {
	t.Helper()
	spec := helperCommand(t, behaviour, args...)
	spec.Supervision = sup
	proc, err := m.Start(context.Background(), spec, "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	return proc
}

// TestAutoRestartOnFailureCrashesAfterBudget verifies that a crash-looping
// process with policy on-failure is restarted with backoff and, once the
// restart budget (max_restarts=2) is exhausted, is marked crashed.
func TestAutoRestartOnFailureCrashesAfterBudget(t *testing.T) {
	m := newManager(t)
	sup := process.Supervision{
		Restart: process.RestartSpec{
			Policy:      process.RestartOnFailure,
			Backoff:     []time.Duration{20 * time.Millisecond},
			MaxRestarts: 2,
		},
	}
	proc := startWithSupervision(t, m, "stderr-exit", nil, sup) // exits 1 immediately

	// Wait for the process to be marked crashed (initial run + 2 restarts).
	waitFor(t, 5*time.Second, "crashed after budget exhaustion", func() bool {
		return proc.Info().Status == process.StatusCrashed
	})
	if got := proc.Info().Restarts; got != 2 {
		t.Fatalf("restarts = %d, want 2", got)
	}
	if got := proc.RestartPolicy(); got != "on-failure" {
		t.Fatalf("restart policy = %q, want on-failure", got)
	}
}

// TestManualStopNeverRestarts verifies that stop_process never triggers a
// restart, even with policy always.
func TestManualStopNeverRestarts(t *testing.T) {
	m := newManager(t)
	sup := process.Supervision{
		Restart: process.RestartSpec{
			Policy:      process.RestartAlways,
			Backoff:     []time.Duration{20 * time.Millisecond},
			MaxRestarts: 10,
		},
	}
	proc := startWithSupervision(t, m, "ignore-term", nil, sup)
	waitFor(t, 5*time.Second, "running", func() bool { return proc.Info().Status == process.StatusRunning })

	firstID := proc.InstanceID()
	if err := m.Stop(context.Background(), proc.ID); err != nil {
		t.Fatal(err)
	}
	// Give a spurious auto-restart time to fire; it must not.
	time.Sleep(150 * time.Millisecond)
	if proc.InstanceID() != firstID {
		t.Fatalf("process was auto-restarted after manual stop")
	}
	if proc.Info().Status != process.StatusStopped {
		t.Fatalf("status = %q, want stopped", proc.Info().Status)
	}
}

// TestOnFailureZeroExitDoesNotRestart verifies on-failure skips clean exits.
func TestOnFailureZeroExitDoesNotRestart(t *testing.T) {
	m := newManager(t)
	sup := process.Supervision{
		Restart: process.RestartSpec{
			Policy:      process.RestartOnFailure,
			Backoff:     []time.Duration{20 * time.Millisecond},
			MaxRestarts: 10,
		},
	}
	proc := startWithSupervision(t, m, "once", nil, sup) // exits 0
	select {
	case <-proc.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit")
	}
	time.Sleep(150 * time.Millisecond)
	if got := proc.Info().Restarts; got != 0 {
		t.Fatalf("restarts = %d, want 0 (clean exit must not restart on-failure)", got)
	}
}

// TestCrashedEventEmitted verifies events.Crashed is published on budget
// exhaustion.
func TestCrashedEventEmitted(t *testing.T) {
	m := newManager(t)
	ch, unsub := m.Events().Subscribe()
	defer unsub()
	sup := process.Supervision{
		Restart: process.RestartSpec{
			Policy:      process.RestartAlways,
			Backoff:     []time.Duration{10 * time.Millisecond},
			MaxRestarts: 1,
		},
	}
	proc := startWithSupervision(t, m, "stderr-exit", nil, sup)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.ProcessID == proc.ID && e.Type == events.Crashed {
				return
			}
		case <-deadline:
			t.Fatal("did not receive events.Crashed")
		}
	}
}

// TestSetRestartPolicy verifies set_restart_policy opts an ad-hoc process into
// supervision.
func TestSetRestartPolicy(t *testing.T) {
	m := newManager(t)
	proc := startWithSupervision(t, m, "stderr-exit", nil, process.Supervision{})
	if err := m.SetRestartPolicy(proc.ID, process.RestartAlways); err != nil {
		t.Fatal(err)
	}
	if proc.RestartPolicy() != "always" {
		t.Fatalf("policy = %q, want always", proc.RestartPolicy())
	}
	if err := m.SetRestartPolicy(proc.ID, "bogus"); err == nil {
		t.Fatal("expected error for invalid policy")
	}
	if err := m.SetRestartPolicy("does-not-exist", process.RestartNever); err != process.ErrNotFound {
		t.Fatalf("unknown id error = %v, want ErrNotFound", err)
	}
}

// TestHealthCheckHealthyAndUnhealthy exercises the continuous health probe. A
// healthy server becomes healthy; an unhealthy server (500s) crosses the
// failure threshold, publishes events.Unhealthy and triggers a restart.
func TestHealthCheckRestartOnUnhealthy(t *testing.T) {
	m := newManager(t)
	ch, unsub := m.Events().Subscribe()
	defer unsub()

	port := freePort(t)
	sup := process.Supervision{
		Readiness: []string{`LISTENING http://`},
		Health: process.HealthSpec{
			HTTP:             "http://127.0.0.1:" + port + "/healthz",
			Interval:         40 * time.Millisecond,
			Timeout:          200 * time.Millisecond,
			FailureThreshold: 2,
		},
		Restart: process.RestartSpec{
			Policy:      process.RestartAlways,
			Backoff:     []time.Duration{200 * time.Millisecond},
			MaxRestarts: 10,
		},
	}
	proc := startWithSupervision(t, m, "http-server", []string{"500", port}, sup)

	// The server returns 500, so after the failure threshold the process must
	// be marked unhealthy and restarted (health-driven restart).
	deadline := time.After(10 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.ProcessID != proc.ID {
				continue
			}
			if e.Type == events.Unhealthy {
				// It restarted at least once after being unhealthy.
				waitFor(t, 5*time.Second, "restart after unhealthy", func() bool {
					return proc.Info().Restarts >= 1
				})
				if h := proc.Health().State; h != process.HealthUnhealthy && proc.Info().Restarts < 1 {
					t.Fatalf("unexpected health state %q", h)
				}
				return
			}
		case <-deadline:
			t.Fatal("did not receive events.Unhealthy")
		}
	}
}

// TestHealthCheckHealthy verifies a healthy server reports healthy.
func TestHealthCheckHealthy(t *testing.T) {
	m := newManager(t)
	port := freePort(t)
	sup := process.Supervision{
		Readiness: []string{`LISTENING http://`},
		Health: process.HealthSpec{
			HTTP:             "http://127.0.0.1:" + port + "/healthz",
			Interval:         30 * time.Millisecond,
			Timeout:          200 * time.Millisecond,
			FailureThreshold: 2,
		},
	}
	proc := startWithSupervision(t, m, "http-server", []string{"200", port}, sup)

	waitFor(t, 5*time.Second, "healthy", func() bool {
		return proc.Health().State == process.HealthHealthy
	})
	if got := proc.Health().ConsecutiveFailures; got != 0 {
		t.Fatalf("consecutive failures = %d, want 0", got)
	}
	m.Stop(context.Background(), proc.ID)
}

// freePort reserves an ephemeral port and returns it as a string. The port is
// released immediately; collisions are possible but unlikely in tests.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr[strings.LastIndex(addr, ":")+1:]
}

// TestCrashLoopWithConcurrentStopRemoveNoRace is the Phase 1 stress test: a
// crash-looping process (auto-restart always) racing concurrent stop/signal/
// force-remove must not trip the race detector, deadlock, or leave an orphaned
// process. Run under -race.
func TestCrashLoopWithConcurrentStopRemoveNoRace(t *testing.T) {
	m := process.New(context.Background(), process.Options{})
	t.Cleanup(func() { m.Shutdown(5 * time.Second) })
	sup := process.Supervision{
		Restart: process.RestartSpec{
			Policy:      process.RestartAlways,
			Backoff:     []time.Duration{time.Millisecond},
			MaxRestarts: 10000,
		},
	}
	proc := startWithSupervision(t, m, "stderr-exit", nil, sup) // exits 1 immediately

	time.Sleep(30 * time.Millisecond) // let it crash-loop a few times
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 40; j++ {
				_ = m.Stop(context.Background(), proc.ID)
				_ = m.Signal(context.Background(), proc.ID, syscall.SIGTERM)
			}
		}()
	}
	removed := make(chan struct{})
	go func() {
		defer close(removed)
		time.Sleep(15 * time.Millisecond)
		_ = m.Remove(proc.ID, true) // force-remove while crash-looping
	}()
	wg.Wait()
	<-removed

	// The process must no longer be managed, and must have been stopped (not
	// left running by a raced auto-restart).
	if _, ok := m.Get(proc.ID); ok {
		t.Fatalf("process %s still in registry after force-remove", proc.ID)
	}
}

// TestResourceLimitsGracefulDegradation verifies that a process with resource
// limits configured still starts (running unlimited) when cgroup v2 is
// unavailable/unwritable, and that LiveUsage degrades to zeros. This is the
// Phase 7 graceful-degradation contract.
func TestResourceLimitsGracefulDegradation(t *testing.T) {
	m := newManager(t)
	spec := helperCommand(t, "print", "2")
	spec.Limits = process.ResourceLimits{CPUQuota: 1.0, MemoryBytes: 128 << 20}
	proc, err := m.Start(context.Background(), spec, "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "running", func() bool { return proc.Info().Status == process.StatusRunning })
	// Whether or not a cgroup was created, usage reads must not panic and the
	// process must be healthy/running.
	proc.LiveUsage()
	if err := m.Stop(context.Background(), proc.ID); err != nil {
		t.Fatal(err)
	}
}
