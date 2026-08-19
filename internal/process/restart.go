package process

import (
	"context"
	"time"

	"agent-runtime/internal/events"
)

// maybeScheduleRestart is the supervisor decision point, called from waitLoop
// after a process exits. A manual stop (stopReq set) never auto-restarts,
// regardless of policy. When the budget is exhausted the process is marked
// crashed instead.
func (m *Manager) maybeScheduleRestart(proc *ManagedProcess, inst *instance, exitCode int) {
	if inst.stopReq.Load() {
		return
	}
	pol := proc.restartPolicy()
	if pol == "" || pol == RestartNever {
		return
	}
	if pol == RestartOnFailure && exitCode == 0 {
		return
	}
	delay, ok := proc.bumpBackoff(time.Now())
	if !ok {
		m.markCrashed(proc, inst, "restart budget exhausted")
		return
	}
	m.scheduleRestart(proc, inst.id, delay)
}

// scheduleRestart arms a one-shot restart timer. The actual start happens on a
// separate goroutine so waitLoop never blocks and the timer loses the race to a
// concurrent stop/remove/restart.
func (m *Manager) scheduleRestart(proc *ManagedProcess, instID string, delay time.Duration) {
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-m.rootCtx.Done():
			return
		}
		m.doRestart(proc, instID)
	}()
}

// doRestart is the guarded restart action. It serializes on proc.startMu so it
// can never race a manual Restart into a double start, and it loses the race if
// the process was removed or already restarted (instance ID changed).
func (m *Manager) doRestart(proc *ManagedProcess, instID string) {
	proc.startMu.Lock()
	defer proc.startMu.Unlock()

	proc.mu.Lock()
	if proc.removed {
		proc.mu.Unlock()
		return
	}
	cur := proc.inst
	if cur == nil || cur.id != instID {
		proc.mu.Unlock()
		return
	}
	grace := proc.grace
	proc.mu.Unlock()

	// Health-driven restarts fire while the instance is still running: stop it
	// first so we never run two instances. Exit-driven restarts find the process
	// already terminal and stopInstance is a no-op.
	if info := proc.Info(); info.Status == StatusRunning || info.Status == StatusStarting {
		stopCtx, cancel := context.WithTimeout(context.Background(), grace+3*time.Second)
		_ = m.stopInstance(proc, stopCtx)
		cancel()
	}

	proc.mu.Lock()
	proc.restarts++
	proc.mu.Unlock()

	m.Events().Publish(events.Event{
		Type: events.Restarted, ProcessID: proc.ID, InstanceID: instID,
		Timestamp: time.Now(),
	})
	m.opts.Logger.Debug("auto-restart", "process_id", proc.ID, "instance_id", instID, "grace", grace)
	if err := m.startInstance(proc, grace); err != nil {
		m.opts.Logger.Warn("auto-restart failed", "process_id", proc.ID, "error", err)
	}
}

// markCrashed declares a process crashed (budget exhausted / unrecoverable),
// publishes events.Crashed and stops supervising it. The instance is already
// terminal.
func (m *Manager) markCrashed(proc *ManagedProcess, inst *instance, reason string) {
	proc.mu.Lock()
	proc.crashed.Store(true)
	proc.mu.Unlock()
	proc.markUnhealthy(proc.healthSnapshot().ConsecutiveFailures + 1)
	m.Events().Publish(events.Event{
		Type: events.Crashed, ProcessID: proc.ID, InstanceID: inst.id,
		Timestamp: time.Now(),
		Payload:   map[string]any{"reason": reason},
	})
	m.opts.Logger.Warn("process crashed", "process_id", proc.ID, "reason", reason)
}

// bumpBackoff advances the restart budget and returns the backoff delay for the
// next restart, or ok=false when the budget is exhausted. The window rolls: a
// process stable for a full backoffWindow resets its budget.
func (p *ManagedProcess) bumpBackoff(now time.Time) (time.Duration, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	b := &p.backoff
	if b.windowStart.IsZero() || now.Sub(b.windowStart) > backoffWindow {
		b.windowStart = now
		b.restarts = 0
		b.step = 0
	}
	if max := p.supervision.Restart.MaxRestarts; max > 0 && b.restarts >= max {
		return 0, false
	}
	steps := p.supervision.Restart.Backoff
	var delay time.Duration
	if len(steps) > 0 {
		idx := b.step
		if idx >= len(steps) {
			idx = len(steps) - 1
		}
		delay = steps[idx]
		if b.step < len(steps) {
			b.step++
		}
	}
	b.restarts++
	return delay, true
}
