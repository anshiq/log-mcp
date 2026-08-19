package process

import (
	"regexp"
	"time"
)

// RestartPolicy is the declarative auto-restart policy for a managed process.
type RestartPolicy string

const (
	// RestartNever disables auto-restart (the default).
	RestartNever RestartPolicy = "never"
	// RestartOnFailure restarts only when the process exits non-zero.
	RestartOnFailure RestartPolicy = "on-failure"
	// RestartAlways restarts regardless of exit code (but never after a manual
	// stop or shutdown).
	RestartAlways RestartPolicy = "always"
)

// HealthSpec configures the continuous health probe. Exactly one of HTTP or TCP
// is normally set; with neither set the process has no health checking.
type HealthSpec struct {
	HTTP             string        // "http://localhost:8080/healthz"
	TCP              string        // "localhost:8080"
	Interval         time.Duration // probe interval
	Timeout          time.Duration // per-probe timeout
	FailureThreshold int           // consecutive failures that mark unhealthy
}

// RestartSpec configures auto-restart with exponential backoff.
type RestartSpec struct {
	Policy RestartPolicy
	// Backoff is the capped-exponential step schedule. The last step is used
	// for all subsequent restarts. Empty disables the delay (immediate).
	Backoff []time.Duration
	// MaxRestarts caps restarts within a rolling 10-minute window. 0 disables
	// the budget (restart forever).
	MaxRestarts int
}

// Supervision bundles the declarative per-process supervision policy: the armed
// readiness patterns, the health probe, and the restart policy. It is immutable
// after Start.
type Supervision struct {
	// Readiness is the armed readiness regex strings (empty means no readiness
	// gating: the process is considered ready as soon as it is running).
	Readiness []string
	Health    HealthSpec
	Restart   RestartSpec

	ready []*regexp.Regexp // compiled Readiness
}

// CompileReadiness compiles the armed readiness patterns into the Supervision.
// Invalid patterns are skipped (they were validated at config load; this is a
// defensive second pass for ad-hoc processes).
func (s Supervision) CompileReadiness() Supervision {
	for _, pat := range s.Readiness {
		if re, err := regexp.Compile(pat); err == nil {
			s.ready = append(s.ready, re)
		}
	}
	return s
}

// matchReady reports whether a log line satisfies any armed readiness pattern.
func (s *Supervision) matchReady(line string) bool {
	for _, re := range s.ready {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// restartPolicy returns the current restart policy. It reads under proc.mu
// because set_restart_policy may mutate it after start.
func (p *ManagedProcess) restartPolicy() RestartPolicy {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.supervision.Restart.Policy
}

// Ready reports whether a log line satisfies any armed readiness pattern. It is
// the exported form used by the runtime to build wait_for_log matchers.
func (s Supervision) Ready(line string) bool {
	return s.matchReady(line)
}

// backoffWindow is the rolling window over which the restart budget is counted.
// A process stable for a full window has its budget reset (it "earns back"
// restarts).
const backoffWindow = 10 * time.Minute

// backoffState is the per-process restart budget and backoff cursor. Guarded by
// ManagedProcess.mu.
type backoffState struct {
	restarts    int
	windowStart time.Time
	step        int
}
