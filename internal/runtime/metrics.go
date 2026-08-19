package runtime

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// Metrics exposes runtime-level counters for the optional Prometheus scrape
// endpoint (runtime.metrics listen address). It is stdlib-only: counters are
// atomic and the handler renders Prometheus text format directly. The
// orchestrator subscribes to the events bus and calls IncCrash / IncRestart on
// process.crashed / process.restarted events.
type Metrics struct {
	startedAt time.Time
	crashes   atomic.Uint64
	restarts  atomic.Uint64
}

// NewMetrics returns a Metrics with the start time stamped now.
func NewMetrics() *Metrics {
	return &Metrics{startedAt: time.Now()}
}

// IncCrash records one process.crashed event.
func (m *Metrics) IncCrash() { m.crashes.Add(1) }

// IncRestart records one process.restarted event.
func (m *Metrics) IncRestart() { m.restarts.Add(1) }

// Crashes returns the number of recorded crash events.
func (m *Metrics) Crashes() uint64 { return m.crashes.Load() }

// Restarts returns the number of recorded restart events.
func (m *Metrics) Restarts() uint64 { return m.restarts.Load() }

// Handler returns an http.Handler serving the counters in Prometheus text
// format at the /metrics path.
func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprint(w, "# HELP agent_runtime_crashes_total Total process.crashed events recorded.\n")
		fmt.Fprint(w, "# TYPE agent_runtime_crashes_total counter\n")
		fmt.Fprintf(w, "agent_runtime_crashes_total %d\n", m.crashes.Load())
		fmt.Fprint(w, "# HELP agent_runtime_restarts_total Total process.restarted events recorded.\n")
		fmt.Fprint(w, "# TYPE agent_runtime_restarts_total counter\n")
		fmt.Fprintf(w, "agent_runtime_restarts_total %d\n", m.restarts.Load())
		fmt.Fprint(w, "# HELP agent_runtime_uptime_seconds Seconds since the runtime started.\n")
		fmt.Fprint(w, "# TYPE agent_runtime_uptime_seconds gauge\n")
		fmt.Fprintf(w, "agent_runtime_uptime_seconds %d\n", int64(time.Since(m.startedAt).Seconds()))
	})
}
