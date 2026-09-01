package process

import (
	"net"
	"net/http"
	"time"

	"agent-runtime/internal/events"
)

// HealthState is the reported health of a managed process.
type HealthState string

const (
	// HealthUnknown is the state before the first probe (or when no health
	// check is configured).
	HealthUnknown HealthState = "unknown"
	HealthHealthy HealthState = "healthy"
	// HealthUnhealthy means the failure threshold was crossed; the process is
	// either being restarted or left crashed.
	HealthUnhealthy HealthState = "unhealthy"
)

// HealthSnapshot is a point-in-time view of a process's health.
type HealthSnapshot struct {
	State               HealthState
	ConsecutiveFailures int
	LastLatency         time.Duration
	LastProbe           time.Time
}

// BackoffInfo is a point-in-time view of the restart budget.
type BackoffInfo struct {
	Restarts      int
	BudgetResetAt time.Time
}

func (p *ManagedProcess) healthSnapshot() HealthSnapshot {
	if s := p.health.Load(); s != nil {
		return *s
	}
	return HealthSnapshot{State: HealthUnknown}
}

func (p *ManagedProcess) setHealth(s HealthSnapshot) {
	cp := s
	p.health.Store(&cp)
}

// Health returns the current health snapshot.
func (p *ManagedProcess) Health() HealthSnapshot {
	return p.healthSnapshot()
}

// Ready reports whether the current instance has reached readiness (always true
// when no readiness patterns are armed and the instance is running).
func (p *ManagedProcess) Ready() bool {
	return p.ready.Load()
}

// RestartPolicy returns the configured restart policy string.
func (p *ManagedProcess) RestartPolicy() string {
	return string(p.restartPolicy())
}

// BackoffInfo returns the current restart-budget view.
func (p *ManagedProcess) BackoffInfo() BackoffInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	return BackoffInfo{Restarts: p.backoff.restarts, BudgetResetAt: p.backoff.windowStart}
}

// recordHealth updates the health snapshot for a single probe result and
// returns the new consecutive-failure count.
func (p *ManagedProcess) recordHealth(ok bool, latency time.Duration) int {
	old := p.healthSnapshot()
	ns := HealthSnapshot{LastLatency: latency, LastProbe: time.Now()}
	if ok {
		ns.State = HealthHealthy
		ns.ConsecutiveFailures = 0
	} else {
		ns.ConsecutiveFailures = old.ConsecutiveFailures + 1
	}
	p.setHealth(ns)
	return ns.ConsecutiveFailures
}

// markUnhealthy sets the health state to unhealthy (used once the failure
// threshold is crossed).
func (p *ManagedProcess) markUnhealthy(fails int) {
	s := p.healthSnapshot()
	p.setHealth(HealthSnapshot{
		State: HealthUnhealthy, ConsecutiveFailures: fails,
		LastLatency: s.LastLatency, LastProbe: s.LastProbe,
	})
}

// resetHealth resets health and readiness for a fresh instance.
func (p *ManagedProcess) resetHealth() {
	p.ready.Store(false)
	p.setHealth(HealthSnapshot{State: HealthUnknown})
}

// healthLoop is the per-process continuous health goroutine. It starts when the
// instance reaches readiness (or immediately, if no readiness is armed) and
// terminates with the instance. One loop per instance — never a shared ticker —
// so a wedging app cannot stall another (invariant 5).
func (m *Manager) healthLoop(proc *ManagedProcess, inst *instance) {
	if !m.waitReady(proc, inst) {
		return
	}
	proc.ready.Store(true)

	h := proc.supervision.Health
	if h.HTTP == "" && h.TCP == "" {
		return
	}
	interval := h.Interval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	threshold := h.FailureThreshold
	if threshold <= 0 {
		threshold = 3
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-inst.done:
			return
		case <-m.rootCtx.Done():
			return
		case <-ticker.C:
			ok, latency := m.probe(proc, timeout)
			fails := proc.recordHealth(ok, latency)
			if ok {
				m.Events().Publish(events.Event{
					Type: events.Healthy, ProcessID: proc.ID, InstanceID: inst.id,
					Timestamp: time.Now(),
				})
				continue
			}
			if fails >= threshold {
				proc.markUnhealthy(fails)
				m.Events().Publish(events.Event{
					Type: events.Unhealthy, ProcessID: proc.ID, InstanceID: inst.id,
					Timestamp: time.Now(),
					Payload:   map[string]any{"consecutive_failures": fails},
				})
				m.opts.Logger.Warn("process failed health check", "process_id", proc.ID, "instance_id", inst.id, "consecutive_failures", fails)
				// Policy-driven restart from a health failure. on-failure and
				// always both restart here (a health failure is a failure).
				if pol := proc.restartPolicy(); pol == RestartAlways || pol == RestartOnFailure {
					m.scheduleRestart(proc, inst.id, 0)
				}
				return
			}
		}
	}
}

// waitReady blocks until the current instance emits a line matching an armed
// readiness pattern, or (with none armed) until it is running. It returns false
// if the instance ends before becoming ready.
func (m *Manager) waitReady(proc *ManagedProcess, inst *instance) bool {
	if len(proc.supervision.ready) == 0 {
		select {
		case <-inst.done:
			return false
		default:
			return true
		}
	}
	subID, wake := proc.SubscribeLogs()
	defer proc.UnsubscribeLogs(subID)

	lastID := inst.entryFrom
	scan := func() bool {
		for _, e := range proc.Logs.From(lastID) {
			lastID = e.ID + 1
			if proc.supervision.matchReady(e.Line) {
				return true
			}
		}
		return false
	}
	if scan() {
		return true
	}
	for {
		select {
		case <-inst.done:
			return false
		case <-m.rootCtx.Done():
			return false
		case <-wake:
			if scan() {
				return true
			}
		}
	}
}

// probe performs one HTTP or TCP health check and returns success plus the
// measured latency.
func (m *Manager) probe(proc *ManagedProcess, timeout time.Duration) (bool, time.Duration) {
	h := proc.supervision.Health
	start := time.Now()
	if h.HTTP != "" {
		client := &http.Client{Timeout: timeout}
		resp, err := client.Get(h.HTTP)
		lat := time.Since(start)
		if err != nil {
			return false, lat
		}
		resp.Body.Close()
		return resp.StatusCode >= 200 && resp.StatusCode < 400, lat
	}
	if h.TCP != "" {
		conn, err := net.DialTimeout("tcp", h.TCP, timeout)
		lat := time.Since(start)
		if err != nil {
			return false, lat
		}
		conn.Close()
		return true, lat
	}
	return true, 0
}
