package process

import (
	"os"
	"time"

	"agent-runtime/internal/events"
)

// AdoptRecord describes a process that outlived the runtime that spawned it
// (daemon crash). PID must be a live OS pid; we are not its parent, so we
// cannot reap it or capture its output — we can only signal its process group
// and poll for death. Its log history lives in the durable archive.
type AdoptRecord struct {
	ProcessID  string
	InstanceID string
	Command    string
	WorkDir    string
	Profile    string
	StartedAt  time.Time
	PID        int
}

// adoptedPollInterval is how often liveness is probed for adopted processes.
const adoptedPollInterval = 500 * time.Millisecond

// Adopt re-registers orphaned processes so a fresh runtime can manage them:
// signal their process group, watch them die, record exits and publish events.
// Logs are served from the archive, not captured (the pipes died with the old
// daemon). Returns (adopted, skipped) counts. A record is skipped when its pid
// is not alive, when its process id already exists, or when its workdir no
// longer exists.
func (m *Manager) Adopt(records []AdoptRecord) (adopted, skipped int) {
	for _, rec := range records {
		if rec.PID <= 0 || !pidAlive(rec.PID) {
			skipped++
			continue
		}
		if _, ok := m.Get(rec.ProcessID); ok {
			skipped++
			continue
		}
		if rec.WorkDir != "" {
			if fi, err := os.Stat(rec.WorkDir); err != nil || !fi.IsDir() {
				m.opts.Logger.Warn("adopt: workdir missing; skipping", "process_id", rec.ProcessID, "pid", rec.PID, "workdir", rec.WorkDir)
				skipped++
				continue
			}
		}
		proc := &ManagedProcess{
			ID:          rec.ProcessID,
			Spec:        StartSpec{Command: rec.Command, WorkDir: rec.WorkDir},
			Profile:     rec.Profile,
			supervision: Supervision{}.CompileReadiness(),
			grace:       m.opts.DefaultGrace,
			wakeup:      newBroadcaster(),
			logger:      m.opts.Logger,
		}
		proc.Logs = m.logs.Get(rec.ProcessID, proc.wakeup.ping)

		inst := &instance{
			id:        rec.InstanceID,
			done:      make(chan struct{}),
			grace:     m.opts.DefaultGrace,
			entryFrom: 0,
		}
		inst.snap.Store(&instanceSnapshot{
			status: StatusRunning, pid: rec.PID, started: rec.StartedAt,
		})
		proc.mu.Lock()
		proc.inst = inst
		proc.mu.Unlock()

		m.mu.Lock()
		m.procs[rec.ProcessID] = proc
		m.mu.Unlock()

		m.opts.Logger.Warn("adopted orphaned process",
			"process_id", rec.ProcessID, "instance_id", rec.InstanceID,
			"pid", rec.PID, "command", rec.Command)
		go m.adoptedWaitLoop(proc, inst)
		adopted++
	}
	return adopted, skipped
}

// adoptedWaitLoop polls liveness of an adopted process and finalizes the
// instance when it dies. We are not the parent, so there is no cmd.Wait and no
// output capture; the exit code is unknown (-1).
func (m *Manager) adoptedWaitLoop(proc *ManagedProcess, inst *instance) {
	ticker := time.NewTicker(adoptedPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if pidAlive(inst.pid()) {
				continue
			}
			now := time.Now()
			code := -1
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
			m.maybeEvictExited()
			inst.finish()
			proc.wakeup.ping()
			eventType := events.Exited
			if inst.stopReq.Load() {
				eventType = events.Stopped
			}
			m.Events().Publish(events.Event{
				Type: eventType, ProcessID: proc.ID, InstanceID: inst.id,
				Timestamp: now, Payload: map[string]any{"exit_code": code, "adopted": true},
			})
			m.opts.Logger.Debug("adopted process exited",
				"process_id", proc.ID, "instance_id", inst.id, "pid", inst.pid())
			return
		case <-m.rootCtx.Done():
			// The owning runtime is shutting down. The adopted process is not our
			// child, so rootCtx cancellation does not stop it; finalize without
			// claiming an exit so the record stays open for the next adopter.
			proc.mu.Lock()
			inst.snap.Store(&instanceSnapshot{status: StatusStopped, pid: inst.pid(), started: inst.snap.Load().started})
			proc.mu.Unlock()
			inst.finish()
			return
		}
	}
}

// procHandle returns an *os.Process for signalling: the real child when we own
// it, or a synthesized handle from the recorded pid for adopted processes.
func (i *instance) procHandle() *os.Process {
	if i.cmd != nil && i.cmd.Process != nil {
		return i.cmd.Process
	}
	if p := i.pid(); p > 0 {
		return &os.Process{Pid: p}
	}
	return nil
}

// pid returns the recorded OS pid of the current instance, or 0.
func (i *instance) pid() int {
	if s := i.snap.Load(); s != nil {
		return s.pid
	}
	return 0
}

// Alive reports whether an OS pid is a live process (signal-0 probe; false on
// platforms without one, e.g. Windows). Exported for tests and the CLI.
func Alive(pid int) bool { return pidAlive(pid) }
