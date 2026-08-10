package process

import (
	"bufio"
	"context"
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
func (m *Manager) startInstance(ctx context.Context, proc *ManagedProcess, grace time.Duration) error {
	instID := m.newInstanceID()
	if grace <= 0 {
		grace = m.opts.DefaultGrace
	}

	// The command derives from the manager's root context, NOT the caller's
	// context: the process must outlive the MCP request that started it.
	cmd := exec.CommandContext(m.rootCtx, proc.Spec.Command, proc.Spec.Args...)
	cmd.Dir = proc.Spec.WorkDir
	cmd.Env = append(os.Environ(), proc.Spec.Env...)
	setProcAttr(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	cmd.Stdin = stdinR

	inst := &instance{
		id:        instID,
		cmd:       cmd,
		status:    StatusStarting,
		done:      make(chan struct{}),
		grace:     grace,
		stdin:     stdinW,
		stdinR:    stdinR,
		entryFrom: proc.Logs.NextID(),
	}

	proc.mu.Lock()
	proc.inst = inst
	proc.mu.Unlock()

	if err := cmd.Start(); err != nil {
		inst.exitCode = new(int)
		*inst.exitCode = -1
		inst.exited = new(time.Time)
		*inst.exited = time.Now()
		proc.setStatus(StatusFailed)
		stdinW.Close()
		stdinR.Close()
		inst.finish()
		m.Events().Publish(events.Event{
			Type: events.Failed, ProcessID: proc.ID, InstanceID: instID,
			Timestamp: time.Now(), Payload: map[string]any{"error": err.Error()},
		})
		return fmt.Errorf("start %q: %w", proc.Spec.Command, err)
	}

	inst.pid = cmd.Process.Pid
	inst.started = time.Now()
	proc.setStatus(StatusRunning)
	m.opts.Logger.Debug("process started",
		"process_id", proc.ID, "instance_id", instID,
		"pid", inst.pid, "command", proc.Spec.Command, "args", proc.Spec.Args)

	go m.readLoop(proc, inst, stdout, logs.StreamStdout)
	go m.readLoop(proc, inst, stderr, logs.StreamStderr)
	go m.waitLoop(proc, inst)

	m.Events().Publish(events.Event{
		Type: events.Started, ProcessID: proc.ID, InstanceID: instID,
		Timestamp: inst.started, Payload: map[string]any{"pid": inst.pid},
	})
	return nil
}

// readLoop consumes one output stream line-by-line. It never emits managed
// process output to the runtime's own logger: managed logs live only in the
// bounded per-process buffers.
func (m *Manager) readLoop(proc *ManagedProcess, inst *instance, r io.Reader, stream logs.Stream) {
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

// waitLoop waits for the child to exit, records the result and notifies
// waiters. The stdout/stderr readers run independently and may finish slightly
// after this returns.
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

	proc.mu.Lock()
	inst.exitCode = &code
	inst.exited = &now
	if inst.stopReq.Load() {
		inst.status = StatusStopped
	} else {
		inst.status = StatusExited
	}
	proc.mu.Unlock()

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
