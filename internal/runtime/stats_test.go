package runtime_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agent-runtime/internal/logs"
	"agent-runtime/internal/runtime"
	"agent-runtime/pkg/api"
)

// statsSink is a fake logs.Sink that also exposes a drop counter.
type statsSink struct {
	dropped uint64
}

func (s *statsSink) Append(string, logs.Entry) {}
func (s *statsSink) Query(string, logs.Query) ([]logs.Entry, error) {
	return nil, nil
}
func (s *statsSink) DeleteProcess(string) error { return nil }
func (s *statsSink) RecordInstance(string, string, string, string, string, int64, int64, *int64, *int64) error {
	return nil
}
func (s *statsSink) Close() error    { return nil }
func (s *statsSink) Dropped() uint64 { return s.dropped }

// gatedForwarder is a Forwarder whose consumer is gated: its bounded queue
// overflows and its drop counter grows while Append stays non-blocking.
type gatedForwarder struct {
	queue chan logs.Entry
	gate  chan struct{}
	mu    sync.Mutex
	drops uint64
}

func newGatedForwarder(size int, gate chan struct{}) *gatedForwarder {
	f := &gatedForwarder{queue: make(chan logs.Entry, size), gate: gate}
	go f.loop()
	return f
}

func (f *gatedForwarder) loop() {
	for e := range f.queue {
		<-f.gate
		_ = e
	}
}

func (f *gatedForwarder) Forward(_ string, e logs.Entry) {
	select {
	case f.queue <- e:
	default:
		f.mu.Lock()
		f.drops++
		f.mu.Unlock()
	}
}

func (f *gatedForwarder) Dropped() uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.drops
}

// TestBuildStatsReportsForwardDrops exercises the real wiring a runtime_stats
// call will use: real managed processes from the manager plus a TeeSink whose
// forwarder queue is full. The stats must report non-zero forward drops while
// Appends stay non-blocking, and read the primary sink's drop counter through
// the tee.
func TestBuildStatsReportsForwardDrops(t *testing.T) {
	rt := newRuntime(t)
	if _, err := rt.Start(context.Background(), api.StartRequest{Command: helperPath, Args: []string{"print", "1"}}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, 5*time.Second, "process running", func() bool {
		procs := rt.Manager().List()
		return len(procs) == 1 && procs[0].Info().Status == "running"
	})
	// Let the helper's print lines land in the ring buffers.
	time.Sleep(150 * time.Millisecond)

	gate := make(chan struct{})
	slow := newGatedForwarder(1, gate)
	primary := &statsSink{dropped: 7}
	tee := logs.NewTee(primary, slow)

	for i := 0; i < 200; i++ {
		tee.Append("proc", logs.Entry{ID: uint64(i), Line: "line"})
	}
	close(gate)

	stats := runtime.BuildStats(rt.Manager().List(), tee, runtime.StatsOptions{
		StartedAt: time.Now().Add(-time.Hour),
	})

	if stats.ForwardDropped == 0 {
		t.Error("expected forward drops > 0 from a full forwarder queue")
	}
	if stats.LogStoreDropped != 7 {
		t.Errorf("LogStoreDropped = %d, want 7 (read through the tee)", stats.LogStoreDropped)
	}
	if stats.TotalProcesses != 1 {
		t.Errorf("TotalProcesses = %d, want 1", stats.TotalProcesses)
	}
	if stats.ProcessesByStatus["running"] != 1 {
		t.Errorf("ProcessesByStatus = %+v, want 1 running", stats.ProcessesByStatus)
	}
	if len(stats.Processes) != 1 || stats.Processes[0].StdoutLines == 0 {
		t.Errorf("per-process summary missing stdout line counts: %+v", stats.Processes)
	}
	if stats.UptimeSeconds < 3599 || stats.UptimeSeconds > 3601 {
		t.Errorf("UptimeSeconds = %d, want ~3600", stats.UptimeSeconds)
	}
}

func TestBuildStatsReadsSinkDropCounter(t *testing.T) {
	stats := runtime.BuildStats(nil, &statsSink{dropped: 42}, runtime.StatsOptions{})
	if stats.LogStoreDropped != 42 {
		t.Errorf("LogStoreDropped = %d, want 42", stats.LogStoreDropped)
	}
}

func TestBuildStatsSubscriptionDropsAndUptime(t *testing.T) {
	var drops atomic.Uint64
	drops.Store(5)
	stats := runtime.BuildStats(nil, nil, runtime.StatsOptions{
		StartedAt:         time.Now().Add(-2 * time.Minute),
		SubscriptionDrops: func() uint64 { return drops.Load() },
	})
	if stats.SubscriptionDrops != 5 {
		t.Errorf("SubscriptionDrops = %d, want 5", stats.SubscriptionDrops)
	}
	if stats.UptimeSeconds < 119 || stats.UptimeSeconds > 121 {
		t.Errorf("UptimeSeconds = %d, want ~120", stats.UptimeSeconds)
	}
}
