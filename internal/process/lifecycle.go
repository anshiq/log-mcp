package process

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"agent-runtime/internal/events"
	"agent-runtime/internal/logs"
)

const maxPendingLine = 256 * 1024

// startInstance launches a new run of an existing logical process. It returns
// as soon as the child has started; output capture continues asynchronously.
// The caller's context is deliberately not used: the command derives from the
// manager's root context so the process outlives the MCP request that started
// it.
func (m *Manager) startInstance(proc *ManagedProcess, grace time.Duration) error {
	instID := m.newInstanceID()
	if grace <= 0 {
		grace = m.opts.DefaultGrace
	}

	// The command derives from the manager's root context, NOT the caller's
	// context: the process must outlive the MCP request that started it.
	cmd := exec.CommandContext(m.rootCtx, proc.Spec.Command, proc.Spec.Args...)
	cmd.Dir = proc.Spec.WorkDir
	cmd.Env = proc.Spec.Env
	setProcAttr(cmd)

	// Child stdout/stderr are routed through in-memory pipes assigned as
	// cmd.Stdout/cmd.Stderr (not StdoutPipe/StderrPipe). exec then runs its own
	// copy goroutines, which cmd.Wait() awaits before returning: every byte the
	// child writes is guaranteed to reach our readLoops before Wait completes.
	// With StdoutPipe, Wait() would close the pipe read ends the moment the
	// child exits, racing the readers and losing their final buffered lines.
	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	cmd.Stdin = stdinR

	inst := &instance{
		id:        instID,
		cmd:       cmd,
		done:      make(chan struct{}),
		grace:     grace,
		stdin:     stdinW,
		stdinR:    stdinR,
		stdoutW:   stdoutW,
		stderrW:   stderrW,
		entryFrom: proc.Logs.NextID(),
	}
	inst.snap.Store(&instanceSnapshot{status: StatusStarting})

	proc.mu.Lock()
	proc.inst = inst
	proc.mu.Unlock()

	if err := cmd.Start(); err != nil {
		now := time.Now()
		code := -1
		proc.mu.Lock()
		inst.snap.Store(&instanceSnapshot{
			status: StatusFailed, exited: &now, exitCode: &code,
		})
		proc.mu.Unlock()
		stdinW.Close()
		stdinR.Close()
		// No copy goroutines or readers were spawned; close the pipes so any
		// (nonexistent) reader would unblock and the pipe can be GC'd.
		stdoutW.Close()
		stderrW.Close()
		inst.finish()
		m.Events().Publish(events.Event{
			Type: events.Failed, ProcessID: proc.ID, InstanceID: instID,
			Timestamp: time.Now(), Payload: map[string]any{"error": err.Error()},
		})
		return fmt.Errorf("start %q: %w", proc.Spec.Command, err)
	}

	now := time.Now()
	proc.mu.Lock()
	inst.snap.Store(&instanceSnapshot{status: StatusRunning, pid: cmd.Process.Pid, started: now})
	proc.mu.Unlock()
	m.recordInstance(proc, inst, now, nil, nil)
	m.opts.Logger.Debug("process started",
		"process_id", proc.ID, "instance_id", instID,
		"pid", cmd.Process.Pid, "command", proc.Spec.Command, "args", proc.Spec.Args)

	inst.readers.Add(2)
	go m.readLoop(proc, inst, stdoutR, logs.StreamStdout)
	go m.readLoop(proc, inst, stderrR, logs.StreamStderr)
	go m.waitLoop(proc, inst)

	m.Events().Publish(events.Event{
		Type: events.Started, ProcessID: proc.ID, InstanceID: instID,
		Timestamp: now, Payload: map[string]any{"pid": cmd.Process.Pid},
	})
	return nil
}

// readLoop consumes one output stream line-by-line. It never emits managed
// process output to the runtime's own logger: managed logs live only in the
// bounded per-process buffers.
func (m *Manager) readLoop(proc *ManagedProcess, inst *instance, r io.Reader, stream logs.Stream) {
	defer inst.readers.Done()
	br := bufio.NewReaderSize(r, 64*1024)
	var pending string
	for {
		chunk, err := br.ReadString('\n')
		if chunk != "" {
			pending += chunk
			if len(pending) > maxPendingLine {
				proc.Logs.Append(stream, strings.TrimSuffix(pending, "\n"))
				pending = ""
			} else {
				pending = splitLines(proc.Logs, stream, pending)
			}
		}
		if err != nil {
			if pending != "" {
				pending = splitLines(proc.Logs, stream, pending)
				if pending != "" {
					proc.Logs.Append(stream, strings.TrimSuffix(pending, "\n"))
				}
			}
			return
		}
	}
}

// splitLines emits all newline-terminated complete lines from pending, returning
// any incomplete trailing fragment to be completed by the next read.
func splitLines(logs *logs.ProcessLogs, stream logs.Stream, pending string) string {
	for {
		idx := strings.IndexByte(pending, '\n')
		if idx < 0 {
			return pending
		}
		line := pending[:idx]
		line = strings.TrimSuffix(line, "\r")
		logs.Append(stream, line)
		pending = pending[idx+1:]
	}
}

// recordInstance writes an instance lifecycle record to the durable sink, if
// one is configured. exitedAt/exitCode are nil on the start record and set on
// the exit record. Failures are logged at debug level only: instance metadata
// is auxiliary and must never affect process management.
func (m *Manager) recordInstance(proc *ManagedProcess, inst *instance, startedAt time.Time, exitedAt *time.Time, exitCode *int) {
	if m.sink == nil {
		return
	}
	var exNanos *int64
	if exitedAt != nil {
		v := exitedAt.UnixNano()
		exNanos = &v
	}
	var code *int64
	if exitCode != nil {
		v := int64(*exitCode)
		code = &v
	}
	_ = m.sink.RecordInstance(proc.ID, inst.id, proc.Spec.Command, proc.Spec.WorkDir, proc.Profile, startedAt.UnixNano(), exNanos, code)
}

// waitLoop waits for the child to exit, records the result and notifies
// waiters. It joins the stdout/stderr readers after cmd.Wait() so final lines
// are flushed into the log buffers before done closes and the Exited/Stopped
// event fires.
func (m *Manager) waitLoop(proc *ManagedProcess, inst *instance) {
	err := inst.cmd.Wait()
	// The child has exited; release both ends of the stdin pipe so the fds are
	// not leaked. (Stdin is an *os.File, so exec owns no copy goroutine here and
	// Wait returns promptly.)
	proc.CloseStdin()
	proc.closeStdinRead()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	now := time.Now()

	// cmd.Wait() has awaited exec's copy goroutines, so every byte the child
	// wrote is now buffered in our io.Pipes. Close the write ends so the
	// readers see EOF, then join them: this guarantees the final lines are
	// flushed into the log buffers before done closes and the exit event fires.
	inst.closeWriters()
	inst.readers.Wait()

	proc.mu.Lock()
	old := inst.snap.Load()
	status := StatusExited
	if inst.stopReq.Load() {
		status = StatusStopped
	}
	inst.snap.Store(&instanceSnapshot{
		status: status, pid: old.pid, started: old.started,
		exited: &now, exitCode: &code,
	})
	proc.mu.Unlock()
	m.recordInstance(proc, inst, old.started, &now, &code)

	// Evict oldest terminal processes beyond the cap BEFORE done closes, so
	// once Done is observable the registry and log buffers are already bounded
	// for the daemon's lifetime.
	m.maybeEvictExited()
	inst.finish()
	proc.wakeup.ping() // wake waiters so they can re-scan for final lines

	eventType := events.Exited
	if inst.stopReq.Load() {
		eventType = events.Stopped
	}
	m.Events().Publish(events.Event{
		Type: eventType, ProcessID: proc.ID, InstanceID: inst.id,
		Timestamp: now, Payload: map[string]any{"exit_code": code},
	})
	m.opts.Logger.Debug("process exited",
		"process_id", proc.ID, "instance_id", inst.id, "exit_code", code)
}

// broadcaster fans out "new log entry" wakeups to a small set of subscribers.
// Each subscriber channel is buffered with capacity 1 and never blocks
// producers; waiters use the ring buffer as the source of truth and only need
// a wakeup nudge, so dropped pings are harmless.
type broadcaster struct {
	mu     sync.Mutex
	subs   map[uint64]chan struct{}
	nextID uint64
}

func newBroadcaster() *broadcaster {
	return &broadcaster{subs: make(map[uint64]chan struct{})}
}

func (b *broadcaster) subscribe() (uint64, <-chan struct{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.nextID
	b.nextID++
	ch := make(chan struct{}, 1)
	b.subs[id] = ch
	return id, ch
}

func (b *broadcaster) unsubscribe(id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch, ok := b.subs[id]; ok {
		close(ch)
		delete(b.subs, id)
	}
}

func (b *broadcaster) ping() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
