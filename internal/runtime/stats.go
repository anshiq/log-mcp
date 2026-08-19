package runtime

import (
	"time"

	"agent-runtime/internal/logs"
	"agent-runtime/internal/process"
	"agent-runtime/pkg/api"
)

// DropCounter is implemented by sinks that count dropped writes (e.g. the
// SQLite archive's full-queue drops). It lets runtime_stats report whether the
// durable archive is keeping up.
type DropCounter interface {
	Dropped() uint64
}

// StatsOptions carries optional inputs to BuildStats.
type StatsOptions struct {
	// StartedAt is when the runtime started; UptimeSeconds is derived from it.
	// Zero means uptime is not reported.
	StartedAt time.Time
	// SubscriptionDrops, when non-nil, is probed for events dropped by the
	// subscription manager under load (the runtime's SubscriptionManager.Stats
	// exposes such a count).
	SubscriptionDrops func() uint64
}

// BuildStats snapshots the runtime's own health: process counts by status,
// per-process log line counts, the durable archive's drop counter, forwarder
// drops, event-subscription drops and uptime. It is a free function so it
// compiles and tests independently of the Runtime struct; the facade passes
// its manager's process list and its sink.
//
// When sink is a *logs.TeeSink it is unwrapped: forward drops are read from
// the tee's fan-out and the durable archive drop counter from its primary.
func BuildStats(procs []*process.ManagedProcess, sink logs.Sink, opts StatsOptions) api.RuntimeStats {
	stats := api.RuntimeStats{
		ProcessesByStatus: make(map[string]int, len(procs)),
		Processes:         make([]api.ProcessStats, 0, len(procs)),
	}
	if !opts.StartedAt.IsZero() {
		stats.UptimeSeconds = int64(time.Since(opts.StartedAt).Seconds())
	}
	for _, p := range procs {
		info := p.Info()
		stats.TotalProcesses++
		stats.ProcessesByStatus[string(info.Status)]++
		stats.Processes = append(stats.Processes, api.ProcessStats{
			ProcessID:   info.ID,
			Status:      string(info.Status),
			Command:     info.Command,
			Profile:     info.Profile,
			Restarts:    info.Restarts,
			StdoutLines: p.Logs.Count(logs.StreamStdout),
			StderrLines: p.Logs.Count(logs.StreamStderr),
		})
	}
	if tee, ok := sink.(*logs.TeeSink); ok {
		stats.ForwardDropped = tee.ForwardDropped()
		sink = tee.Sink // unwrap: the primary is the durable archive
	}
	if dc, ok := sink.(DropCounter); ok {
		stats.LogStoreDropped = dc.Dropped()
	}
	if opts.SubscriptionDrops != nil {
		stats.SubscriptionDrops = opts.SubscriptionDrops()
	}
	return stats
}
