package process

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"agent-runtime/internal/events"
	"agent-runtime/internal/logs"
)

// Options configures a Manager.
type Options struct {
	LogCapacity        int
	DefaultGrace       time.Duration
	MaxExitedProcesses int
	ExitedTTL          time.Duration
	Events             *events.Bus
	Logger             *slog.Logger
	Sink               logs.Sink
}

// Manager owns all managed processes and is safe for concurrent use.
type Manager struct {
	rootCtx context.Context
	opts    Options
	logs    *logs.Store
	sink    logs.Sink

	mu        sync.RWMutex
	procs     map[string]*ManagedProcess
	nextID    uint64
	instID    uint64
	exitedTTL atomic.Int64
}

// New creates a Manager bound to rootCtx. rootCtx cancellation is the backstop
// for terminating every managed process.
func New(rootCtx context.Context, opts Options) *Manager {
	if opts.LogCapacity <= 0 {
		opts.LogCapacity = 10000
	}
	if opts.DefaultGrace <= 0 {
		opts.DefaultGrace = 5 * time.Second
	}
	if opts.ExitedTTL <= 0 {
		opts.ExitedTTL = 5 * time.Minute
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Events == nil {
		opts.Events = events.New()
	}
	m := &Manager{
		rootCtx: rootCtx,
		opts:    opts,
		logs:    logs.NewStoreWithSink(opts.LogCapacity, opts.Sink),
		sink:    opts.Sink,
		procs:   make(map[string]*ManagedProcess),
	}
	m.exitedTTL.Store(int64(opts.ExitedTTL))
	return m
}

// Start launches a process and returns immediately. The process continues
// running independently of the caller's context.
func (m *Manager) Start(ctx context.Context, spec StartSpec, profile string, grace time.Duration) (*ManagedProcess, error) {
	if err := m.validateSpec(spec); err != nil {
		return nil, err
	}
	id := m.newProcessID()
	if grace <= 0 {
		grace = m.opts.DefaultGrace
	}
	proc := &ManagedProcess{
		ID:          id,
		Spec:        spec,
		Profile:     profile,
		supervision: spec.Supervision.CompileReadiness(),
		grace:       grace,
		wakeup:      newBroadcaster(),
		logger:      m.opts.Logger,
	}
	// The wakeup callback is wired in at construction, before the process is
	// visible to readers, so it can never be mutated concurrently with Appends.
	proc.Logs = m.logs.Get(id, proc.wakeup.ping)

	m.mu.Lock()
	m.procs[id] = proc
	m.mu.Unlock()

	if err := m.startInstance(proc, grace); err != nil {
		m.mu.Lock()
		delete(m.procs, id)
		m.mu.Unlock()
		m.logs.Delete(id)
		return nil, err
	}
	m.sweepExpired()
	return proc, nil
}

// Stop terminates the process gracefully (SIGTERM to its process group), then
// escalates to SIGKILL after the grace period. It never blocks forever. It
// serializes on proc.startMu so it can never race an in-flight auto-restart
// that is mid-start on the same instance.
func (m *Manager) Stop(ctx context.Context, id string) error {
	proc, _ := m.Get(id)
	if proc == nil {
		return ErrNotFound
	}
	proc.startMu.Lock()
	defer proc.startMu.Unlock()
	return m.stopInstance(proc, ctx)
}

// stopInstance terminates the current instance of proc. The caller must hold
// proc.startMu (which serializes it against manual/auto restarts), so
// inst.cmd.Process is stable and never read mid-Start.
func (m *Manager) stopInstance(proc *ManagedProcess, ctx context.Context) error {
	info := proc.Info()
	switch info.Status {
	case StatusStopped, StatusExited, StatusFailed, StatusCreated, StatusCrashed:
		return nil
	}
	proc.setStatus(StatusStopping)
	inst := m.currentInstance(proc)
	if inst == nil {
		return nil
	}
	inst.stopReq.Store(true)
	proc.CloseStdin()

	grace := inst.grace
	if grace <= 0 {
		grace = m.opts.DefaultGrace
	}

	handle := inst.procHandle()
	if handle == nil {
		// Adopted process with no live pid handle: nothing to signal. Mark the
		// instance finished so callers can proceed.
		inst.finish()
		return nil
	}
	sigTerm(handle)
	graceTimer := time.NewTimer(grace)
	defer graceTimer.Stop()
	select {
	case <-inst.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-graceTimer.C:
	}

	sigKill(handle)
	killTimer := time.NewTimer(2 * time.Second)
	defer killTimer.Stop()
	select {
	case <-inst.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-killTimer.C:
		// Last resort: the process is unkillable or in an uninterruptible
		// state. Mark the instance finished so callers can proceed.
		inst.finish()
		return nil
	}
}

// Signal delivers an arbitrary signal to the entire process group of the
// process (mycli -> uv -> python tree). Unlike Stop it does not close stdin,
// set stopReq or escalate; it is a single group-wide delivery.
func (m *Manager) Signal(ctx context.Context, id string, sig syscall.Signal) error {
	proc, _ := m.Get(id)
	if proc == nil {
		return ErrNotFound
	}
	switch info := proc.Info(); info.Status {
	case StatusStopped, StatusExited, StatusFailed, StatusCreated:
		return fmt.Errorf("process %q is not running", id)
	}
	inst := m.currentInstance(proc)
	if inst == nil || inst.procHandle() == nil {
		return fmt.Errorf("process %q is not running", id)
	}
	err := signalGroup(inst.procHandle(), sig)
	// The process may have exited between the status check and the kill; a
	// missing group is a benign race, not an error.
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// Restart stops the current instance and starts a new one with the same
// StartSpec. The logical process ID and its log history are preserved. Manual
// restarts reset the restart budget (backoff) and clear any crashed state, so a
// human can always recover a crashed process.
func (m *Manager) Restart(ctx context.Context, id string) error {
	proc, _ := m.Get(id)
	if proc == nil {
		return ErrNotFound
	}
	proc.startMu.Lock()
	defer proc.startMu.Unlock()

	grace := m.opts.DefaultGrace
	if info := proc.Info(); info.Status == StatusRunning || info.Status == StatusStarting {
		if inst := m.currentInstance(proc); inst != nil && inst.grace > 0 {
			grace = inst.grace
		}
		stopCtx, cancel := context.WithTimeout(context.Background(), grace+3*time.Second)
		err := m.stopInstance(proc, stopCtx)
		cancel()
		if err != nil {
			return err
		}
	}
	proc.mu.Lock()
	proc.restarts++
	proc.backoff = backoffState{} // manual restart resets the budget
	proc.crashed.Store(false)
	proc.mu.Unlock()
	return m.startInstance(proc, grace)
}

// SetRestartPolicy changes the restart policy of an existing process. It is the
// hook for set_restart_policy, letting an ad-hoc process (started without an app
// entry) opt into supervision after the fact. Invalid policies are rejected.
func (m *Manager) SetRestartPolicy(id string, policy RestartPolicy) error {
	proc, ok := m.Get(id)
	if !ok {
		return ErrNotFound
	}
	switch policy {
	case RestartNever, RestartOnFailure, RestartAlways:
	default:
		return fmt.Errorf("invalid restart policy %q (want never|on-failure|always)", policy)
	}
	proc.mu.Lock()
	proc.supervision.Restart.Policy = policy
	proc.mu.Unlock()
	return nil
}

// Remove deletes a process from the registry and frees its log buffers. A
// starting, running or stopping process is refused unless force is true, in
// which case it is stopped first. After removal no goroutine appends to the
// process's buffers: for an already-terminal process the readers and waitLoop
// have finished; for force removal Stop() waits for the instance to finish.
func (m *Manager) Remove(id string, force bool) error {
	proc, ok := m.Get(id)
	if !ok {
		return ErrNotFound
	}
	// Serialize with any in-flight auto-restart (doRestart) so a force remove
	// never races a pending startInstance into an orphaned process that keeps
	// running after removal.
	proc.startMu.Lock()
	defer proc.startMu.Unlock()

	switch info := proc.Info(); info.Status {
	case StatusStarting, StatusRunning, StatusStopping:
		if !force {
			return fmt.Errorf("process %q is %s; use force=true to stop and remove it", id, info.Status)
		}
		stopCtx, cancel := context.WithTimeout(context.Background(), m.opts.DefaultGrace+3*time.Second)
		err := m.stopInstance(proc, stopCtx)
		cancel()
		if err != nil {
			return err
		}
	}
	m.mu.Lock()
	delete(m.procs, id)
	m.mu.Unlock()
	proc.mu.Lock()
	proc.removed = true
	proc.mu.Unlock()
	m.logs.Delete(id)
	if m.opts.Events != nil {
		m.opts.Events.Publish(events.Event{Type: events.Removed, ProcessID: id, Timestamp: time.Now()})
	}
	return nil
}

// maybeEvictExited bounds the registry by removing the oldest terminal
// (exited/stopped/failed) processes beyond the configured cap, oldest first
// (LRU by exit time). Running processes are never touched.
func (m *Manager) maybeEvictExited() {
	cap := m.opts.MaxExitedProcesses
	if cap <= 0 {
		return
	}
	type term struct {
		id   string
		exit time.Time
	}
	m.mu.RLock()
	var terms []term
	for id, p := range m.procs {
		info := p.Info()
		switch info.Status {
		case StatusExited, StatusStopped, StatusFailed, StatusCrashed:
			exit := time.Time{}
			if info.ExitedAt != nil {
				exit = *info.ExitedAt
			}
			terms = append(terms, term{id: id, exit: exit})
		}
	}
	m.mu.RUnlock()
	if len(terms) <= cap {
		return
	}
	sort.Slice(terms, func(i, j int) bool { return terms[i].exit.Before(terms[j].exit) })
	for _, t := range terms[:len(terms)-cap] {
		m.Remove(t.id, false)
	}
}

func (m *Manager) SetExitedTTL(d time.Duration) {
	if d <= 0 {
		return
	}
	m.exitedTTL.Store(int64(d))
	m.opts.ExitedTTL = d
}

func (m *Manager) sweepExpired() {
	ttl := time.Duration(m.exitedTTL.Load())
	if ttl <= 0 {
		return
	}
	cutoff := time.Now().Add(-ttl)
	type expired struct {
		id       string
		instID   string
		exitedAt time.Time
	}
	m.mu.RLock()
	var victims []expired
	for id, p := range m.procs {
		info := p.Info()
		switch info.Status {
		case StatusExited, StatusStopped, StatusFailed, StatusCrashed:
		default:
			continue
		}
		if info.ExitedAt == nil || info.ExitedAt.After(cutoff) {
			continue
		}
		victims = append(victims, expired{id: id, instID: info.InstanceID, exitedAt: *info.ExitedAt})
	}
	m.mu.RUnlock()
	for _, v := range victims {
		proc, ok := m.Get(v.id)
		if !ok {
			continue
		}
		if proc.InstanceID() != v.instID {
			continue
		}
		info := proc.Info()
		switch info.Status {
		case StatusExited, StatusStopped, StatusFailed, StatusCrashed:
		default:
			continue
		}
		if info.ExitedAt == nil || info.ExitedAt.After(cutoff) {
			continue
		}
		m.Remove(v.id, false)
	}
}

func (m *Manager) scheduleExpiry(id, instID string) {
	ttl := time.Duration(m.exitedTTL.Load())
	if ttl <= 0 {
		return
	}
	go func() {
		select {
		case <-time.After(ttl):
		case <-m.rootCtx.Done():
			return
		}
		proc, ok := m.Get(id)
		if !ok {
			return
		}
		if proc.InstanceID() != instID {
			return
		}
		info := proc.Info()
		switch info.Status {
		case StatusExited, StatusStopped, StatusFailed, StatusCrashed:
		default:
			return
		}
		if info.ExitedAt == nil {
			return
		}
		if time.Since(*info.ExitedAt) < ttl {
			return
		}
		m.Remove(id, false)
	}()
}

// Get returns the managed process, if present.
func (m *Manager) Get(id string) (*ManagedProcess, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.procs[id]
	return p, ok
}

// List returns all managed processes sorted by ID.
func (m *Manager) List() []*ManagedProcess {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*ManagedProcess, 0, len(m.procs))
	for _, p := range m.procs {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Logs exposes the shared log store.
func (m *Manager) Logs() *logs.Store { return m.logs }

// Events exposes the shared event bus.
func (m *Manager) Events() *events.Bus { return m.opts.Events }

// Shutdown gracefully stops all managed processes, signalling them all
// concurrently so the wall-clock cost is the slowest single process (bounded by
// timeout), never N x timeout. Returns an error only if a process could not be
// stopped within the overall timeout.
func (m *Manager) Shutdown(timeout time.Duration) error {
	procs := m.List()
	if len(procs) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)
	for _, p := range procs {
		p := p
		g.Go(func() error {
			return m.Stop(ctx, p.ID)
		})
	}
	return g.Wait()
}

func (m *Manager) validateSpec(spec StartSpec) error {
	if spec.Command == "" {
		return errors.New("command must not be empty")
	}
	dir := spec.WorkDir
	if dir == "" {
		dir = "."
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("workdir %q: %w", dir, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("workdir %q is not a directory", dir)
	}
	return nil
}

func (m *Manager) currentInstance(p *ManagedProcess) *instance {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inst
}

func (m *Manager) newProcessID() string {
	id, err := randomID()
	if err != nil {
		m.mu.Lock()
		m.nextID++
		id = fmt.Sprintf("proc_%d", m.nextID)
		m.mu.Unlock()
		return id
	}
	return "proc_" + id
}

func (m *Manager) newInstanceID() string {
	id, err := randomID()
	if err != nil {
		m.mu.Lock()
		m.instID++
		id = fmt.Sprintf("%d", m.instID)
		m.mu.Unlock()
		return "run_" + id
	}
	return "run_" + id
}

func randomID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
