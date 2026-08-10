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
}

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

// instance represents a single run of a process. The logical ManagedProcess
// identity is stable across restarts; instance IDs change on every run.
type instance struct {
	id        string
	cmd       *exec.Cmd
	status    Status
	pid       int
	started   time.Time
	exited    *time.Time
	exitCode  *int
	done      chan struct{}
	doneOnce  sync.Once
	stopReq   atomic.Bool
	grace     time.Duration
	stdin     io.WriteCloser
	stdinR    io.Closer
	stdinMu   sync.Mutex
	stdinEOF  bool
	entryFrom uint64 // first log entry ID that belongs to this instance
}

func (i *instance) finish() {
	i.doneOnce.Do(func() { close(i.done) })
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
}

// Info returns a snapshot of the current instance.
func (p *ManagedProcess) Info() Info {
	p.mu.Lock()
	defer p.mu.Unlock()
	inst := p.inst
	if inst == nil {
		return Info{
			ID: p.ID, Command: p.Spec.Command, Args: p.Spec.Args,
			WorkDir: p.Spec.WorkDir, Profile: p.Profile,
			Status: StatusCreated, Restarts: p.restarts,
		}
	}
	return Info{
		ID: p.ID, InstanceID: inst.id, Status: inst.status,
		Command: p.Spec.Command, Args: p.Spec.Args, WorkDir: p.Spec.WorkDir,
		PID: inst.pid, StartedAt: inst.started, ExitedAt: inst.exited,
		ExitCode: inst.exitCode, Profile: p.Profile, Restarts: p.restarts,
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
	if p.inst != nil {
		p.inst.status = s
	}
	p.mu.Unlock()
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
	if inst == nil || inst.status == StatusFailed || inst.status == StatusStopped ||
		inst.status == StatusExited {
		return ErrAlreadyDead
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
	if inst == nil {
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
	if inst == nil {
		return
	}
	inst.stdinMu.Lock()
	defer inst.stdinMu.Unlock()
	inst.stdinR.Close()
}
