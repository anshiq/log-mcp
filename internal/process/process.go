// Package process owns the lifecycle of managed processes. It is deliberately
// generic: it knows nothing about Node, Java, Python or any specific runtime.
// Runtime "profiles" live in a separate package and only inform this one.
package process

import (
	"errors"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"agent-runtime/internal/logs"
)

// Status is the lifecycle state of a managed process instance.
type Status string

const (
	StatusCreated  Status = "created"
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusStopping Status = "stopping"
	StatusStopped  Status = "stopped"
	StatusExited   Status = "exited"
	StatusFailed   Status = "failed"
	// StatusCrashed means the process exited and its restart budget was
	// exhausted (or no restart was permitted), so supervision gave up. It is a
	// proc-level state reported in Info alongside the instance's terminal
	// status.
	StatusCrashed Status = "crashed"
)

var (
	ErrNotFound    = errors.New("process not found")
	ErrAlreadyDead = errors.New("process is not running")
	ErrStdinClosed = errors.New("stdin is closed")
)

// StartSpec is a generic command specification. No runtime is hard-coded here.
type StartSpec struct {
	Command string
	Args    []string
	WorkDir string
	Env     []string

	// Readiness is the armed readiness regex strings for this process (empty
	// means no readiness gating). Supervision carries the declarative
	// health/restart policy.
	Readiness   []string
	Supervision Supervision

	// Limits are the optional per-app resource caps (Phase 7): cpu quota in
	// cores and memory in bytes. Zero means unlimited for that resource.
	Limits ResourceLimits
}

// ResourceLimits are per-process resource caps (Phase 7, Linux-first via cgroup
// v2, degrading gracefully to unlimited when unsupported).
type ResourceLimits struct {
	CPUQuota    float64 // fraction of a core; 0 = unlimited
	MemoryBytes int64   // bytes; 0 = unlimited
}

func (l ResourceLimits) Enabled() bool { return l.CPUQuota > 0 || l.MemoryBytes > 0 }

// Info is a read-only snapshot of a managed process's current instance.
type Info struct {
	ID         string
	InstanceID string
	Status     Status
	Command    string
	Args       []string
	WorkDir    string
	PID        int
	StartedAt  time.Time
	ExitedAt   *time.Time
	ExitCode   *int
	Profile    string
	Restarts   int
}

// instanceSnapshot is an immutable view of an instance's lifecycle state. It
// lives behind an atomic.Pointer so readers (Info, SendStdin, ...) can observe
// it lock-free; writers build a new snapshot and swap it in under proc.mu to
// avoid lost updates during status transitions.
type instanceSnapshot struct {
	status   Status
	pid      int
	started  time.Time
	exited   *time.Time
	exitCode *int
}

// instance represents a single run of a process. The logical ManagedProcess
// identity is stable across restarts; instance IDs change on every run.
type instance struct {
	id        string
	cmd       *exec.Cmd
	snap      atomic.Pointer[instanceSnapshot]
	done      chan struct{}
	doneOnce  sync.Once
	stopReq   atomic.Bool
	grace     time.Duration
	stdin     io.WriteCloser
	stdinR    io.Closer
	stdinMu   sync.Mutex
	stdinEOF  bool
	stdoutW   io.WriteCloser // write end of the stdout capture pipe
	stderrW   io.WriteCloser // write end of the stderr capture pipe
	entryFrom uint64         // first log entry ID that belongs to this instance
	// readers tracks the stdout/stderr readLoop goroutines for this instance.
	// waitLoop joins it after cmd.Wait() so the instance's done channel (and
	// the Exited/Stopped event) only fires once the readers have flushed their
	// final lines into the log buffers.
	readers sync.WaitGroup

	// jobCleanup closes the Windows Job Object handle for this instance's
	// process tree. With JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE set, closing the
	// last handle terminates every process still in the job (tree-kill). It is
	// a no-op on Unix (the process group handles tree termination).
	jobCleanup func()

	// cgroupPath is the cgroup v2 slice this instance was placed in ("" when
	// none), used for live usage reporting and OOM detection.
	cgroupPath string
	// oomBaseline snapshots the cgroup's oom_kill counter at start so a later
	// OOM can be detected on exit.
	oomBaseline uint64
	// resourceCleanup removes the cgroup slice at teardown (no-op when none).
	resourceCleanup func()
}

func (i *instance) finish() {
	i.doneOnce.Do(func() { close(i.done) })
}

// closeWriters closes the write ends of the stdout/stderr capture pipes so the
// readLoop goroutines see EOF. Called by waitLoop after cmd.Wait() has ensured
// all child output has been copied into the pipes.
func (i *instance) closeWriters() {
	if i.stdoutW != nil {
		i.stdoutW.Close()
	}
	if i.stderrW != nil {
		i.stderrW.Close()
	}
}

// ManagedProcess is the stable, logical identity of a supervised process. Its
// logs survive restarts; status reflects the current instance.
type ManagedProcess struct {
	ID      string
	Spec    StartSpec
	Profile string
	Logs    *logs.ProcessLogs

	mu       sync.Mutex
	inst     *instance
	restarts int
	wakeup   *broadcaster
	logger   *slog.Logger

	// supervision is the declarative policy set once at Start and immutable
	// thereafter (a running process keeps its policy unless set_restart_policy
	// is used).
	supervision Supervision

	// ready is true once the current instance reaches readiness.
	ready atomic.Bool
	// health is the current health snapshot.
	health atomic.Pointer[HealthSnapshot]
	// crashed is set when the restart budget is exhausted or a crash is
	// declared; it makes Info report StatusCrashed.
	crashed atomic.Bool

	// backoff is the restart budget/cursor; guarded by mu.
	backoff backoffState
	// removed marks the process as deleted so pending auto-restart timers lose
	// the race; guarded by mu.
	removed bool
	// grace is the per-instance stop grace, stored so auto-restart can re-apply
	// it; guarded by mu.
	grace time.Duration
	// startMu serializes instance starts (manual Restart vs auto-restart) so
	// two goroutines can never start two instances of the same process.
	startMu sync.Mutex
}

// Info returns a snapshot of the current instance. The instance pointer and
// restarts counter are guarded by proc.mu; the per-instance lifecycle fields
// are read lock-free from the immutable snapshot.
func (p *ManagedProcess) Info() Info {
	p.mu.Lock()
	inst := p.inst
	restarts := p.restarts
	crashed := p.crashed.Load()
	p.mu.Unlock()
	if inst == nil {
		return Info{
			ID: p.ID, Command: p.Spec.Command, Args: p.Spec.Args,
			WorkDir: p.Spec.WorkDir, Profile: p.Profile,
			Status: StatusCreated, Restarts: restarts,
		}
	}
	snap := inst.snap.Load()
	status := snap.status
	if crashed {
		status = StatusCrashed
	}
	return Info{
		ID: p.ID, InstanceID: inst.id, Status: status,
		Command: p.Spec.Command, Args: p.Spec.Args, WorkDir: p.Spec.WorkDir,
		PID: snap.pid, StartedAt: snap.started, ExitedAt: snap.exited,
		ExitCode: snap.exitCode, Profile: p.Profile, Restarts: restarts,
	}
}

// InstanceID returns the current instance ID (empty before first start).
func (p *ManagedProcess) InstanceID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inst == nil {
		return ""
	}
	return p.inst.id
}

// LiveUsage returns the current resource usage (memory bytes and CPU
// nanoseconds) read live from the instance's cgroup v2 slice, or zeros when no
// slice is configured (resource limits off / unsupported).
func (p *ManagedProcess) LiveUsage() (memBytes, cpuNanos int64) {
	p.mu.Lock()
	inst := p.inst
	p.mu.Unlock()
	if inst == nil || inst.cgroupPath == "" {
		return 0, 0
	}
	return readCgroupUsage(inst.cgroupPath)
}

// Done returns a channel closed when the current instance terminates.
func (p *ManagedProcess) Done() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inst == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return p.inst.done
}

// EntryFrom is the ID of the first log entry belonging to the current instance.
func (p *ManagedProcess) EntryFrom() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inst == nil {
		return 0
	}
	return p.inst.entryFrom
}

func (p *ManagedProcess) setStatus(s Status) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inst == nil {
		return
	}
	old := p.inst.snap.Load()
	p.inst.snap.Store(&instanceSnapshot{
		status: s, pid: old.pid, started: old.started,
		exited: old.exited, exitCode: old.exitCode,
	})
}

// SubscribeLogs registers a waiter for new log entries. The returned channel is
// a buffered wakeup: waiters must re-scan the log buffer (the source of truth)
// when woken, and must call UnsubscribeLogs when done.
func (p *ManagedProcess) SubscribeLogs() (uint64, <-chan struct{}) {
	return p.wakeup.subscribe()
}

// UnsubscribeLogs removes a waiter registered by SubscribeLogs.
func (p *ManagedProcess) UnsubscribeLogs(id uint64) {
	p.wakeup.unsubscribe(id)
}

// SendStdin writes to the process's stdin. Only safe while running.
func (p *ManagedProcess) SendStdin(data string) error {
	p.mu.Lock()
	inst := p.inst
	p.mu.Unlock()
	if inst == nil {
		return ErrAlreadyDead
	}
	if snap := inst.snap.Load(); snap.status == StatusFailed || snap.status == StatusStopped ||
		snap.status == StatusExited {
		return ErrAlreadyDead
	}
	if inst.stdin == nil {
		// Adopted process: stdin died with the owning daemon; nothing to write.
		return ErrStdinClosed
	}
	inst.stdinMu.Lock()
	defer inst.stdinMu.Unlock()
	if inst.stdinEOF {
		return ErrStdinClosed
	}
	if _, err := io.WriteString(inst.stdin, data); err != nil {
		return err
	}
	return nil
}

func (p *ManagedProcess) CloseStdin() {
	p.mu.Lock()
	inst := p.inst
	p.mu.Unlock()
	if inst == nil || inst.stdin == nil {
		return
	}
	inst.stdinMu.Lock()
	defer inst.stdinMu.Unlock()
	if inst.stdinEOF {
		return
	}
	inst.stdinEOF = true
	inst.stdin.Close()
}

// closeStdinRead closes the read end of the stdin pipe (our side). Only called
// after the child has exited.
func (p *ManagedProcess) closeStdinRead() {
	p.mu.Lock()
	inst := p.inst
	p.mu.Unlock()
	if inst == nil || inst.stdinR == nil {
		return
	}
	inst.stdinMu.Lock()
	defer inst.stdinMu.Unlock()
	inst.stdinR.Close()
}
