package logs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// recordingSink is a minimal logs.Sink that counts what it receives.
type recordingSink struct {
	mu      sync.Mutex
	appends int
	queries int
	deletes int
	records int
	closed  bool
	dropped uint64
}

func (r *recordingSink) Append(string, Entry) {
	r.mu.Lock()
	r.appends++
	r.mu.Unlock()
}

func (r *recordingSink) Query(string, Query) ([]Entry, error) {
	r.mu.Lock()
	r.queries++
	r.mu.Unlock()
	return nil, nil
}

func (r *recordingSink) DeleteProcess(string) error {
	r.mu.Lock()
	r.deletes++
	r.mu.Unlock()
	return nil
}

func (r *recordingSink) RecordInstance(string, string, string, string, string, int64, int64, *int64, *int64) error {
	r.mu.Lock()
	r.records++
	r.mu.Unlock()
	return nil
}

func (r *recordingSink) Close() error {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	return nil
}

func (r *recordingSink) Dropped() uint64 { return atomic.LoadUint64(&r.dropped) }

// blockingForwarder is a Forwarder whose consumer never completes while the
// gate is held: its bounded queue overflows and its drop counter grows.
type blockingForwarder struct {
	queue chan Entry
	gate  chan struct{}
	mu    sync.Mutex
	drops uint64
}

func newBlockingForwarder(size int, gate chan struct{}) *blockingForwarder {
	b := &blockingForwarder{queue: make(chan Entry, size), gate: gate}
	go b.loop()
	return b
}

func (b *blockingForwarder) loop() {
	for e := range b.queue {
		<-b.gate
		_ = e
	}
}

func (b *blockingForwarder) Forward(_ string, e Entry) {
	select {
	case b.queue <- e:
	default:
		b.mu.Lock()
		b.drops++
		b.mu.Unlock()
	}
}

func (b *blockingForwarder) Dropped() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.drops
}

// TestTeeSinkDropOnBackpressure proves the drop-on-backpressure contract under
// -race: a slow forwarder (its consumer is gated, so its queue overflows)
// never blocks TeeSink.Append, the primary still receives every entry, and the
// overflow is counted.
func TestTeeSinkDropOnBackpressure(t *testing.T) {
	gate := make(chan struct{})
	slow := newBlockingForwarder(1, gate)
	primary := &recordingSink{}
	tee := NewTee(primary, slow)

	const total = 2000
	start := time.Now()
	for i := 0; i < total; i++ {
		tee.Append("proc", Entry{ID: uint64(i), Line: fmt.Sprintf("l%d", i)})
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Append blocked on a slow forwarder: took %v", elapsed)
	}

	primary.mu.Lock()
	gotAppends := primary.appends
	primary.mu.Unlock()
	if gotAppends != total {
		t.Fatalf("primary received %d of %d appends", gotAppends, total)
	}
	if slow.Dropped() == 0 {
		t.Fatal("expected the slow forwarder to drop entries, got 0")
	}
	if tee.ForwardDropped() == 0 {
		t.Fatal("TeeSink.ForwardDropped must aggregate forwarder drops")
	}

	close(gate)
	if err := tee.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestTeeSinkDelegatesAndCloses(t *testing.T) {
	primary := &recordingSink{}
	tee := NewTee(primary)

	tee.Append("p", Entry{ID: 1, Line: "x"})
	if _, err := tee.Query("p", Query{}); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if err := tee.DeleteProcess("p"); err != nil {
		t.Fatalf("DeleteProcess: %v", err)
	}
	if err := tee.RecordInstance("p", "i1", "cmd", "/wd", "gen", 1, 0, nil, nil); err != nil {
		t.Fatalf("RecordInstance: %v", err)
	}
	if err := tee.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	primary.mu.Lock()
	defer primary.mu.Unlock()
	if primary.appends != 1 || primary.queries != 1 || primary.deletes != 1 ||
		primary.records != 1 || !primary.closed {
		t.Fatalf("primary not fully delegated: %+v", primary)
	}
}

func TestTeeSinkNilPrimary(t *testing.T) {
	tee := NewTee(nil) // memory mode with forwarding enabled
	tee.Append("p", Entry{ID: 1})
	if _, err := tee.Query("p", Query{}); err == nil {
		t.Fatal("Query with nil primary must error")
	}
	if err := tee.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestStdoutForwarderPrints(t *testing.T) {
	var buf bytes.Buffer
	f := NewStdoutForwarderTo(&buf)
	f.Forward("p1", Entry{
		ID: 1, Timestamp: time.Unix(0, 0).UTC(), Stream: StreamStdout, Line: "level=info hello",
	})
	f.Forward("p2", Entry{
		ID: 2, Timestamp: time.Unix(0, 0).UTC(), Stream: StreamStderr, Line: "level=error boom",
	})
	f.Close()
	got := buf.String()
	if !strings.Contains(got, "hello") || !strings.Contains(got, "boom") ||
		!strings.Contains(got, "stdout") || !strings.Contains(got, "stderr") {
		t.Fatalf("stdout forwarder output missing expected content: %q", got)
	}
}

func TestOTLPForwarderExportsAndFailsGracefully(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := new(bytes.Buffer)
		_, _ = io.Copy(buf, r.Body)
		mu.Lock()
		bodies = append(bodies, buf.String())
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := newOTLPForwarder(srv.URL+"/v1/logs", srv.Client(), nil, 16, 2, 10*time.Millisecond)
	defer f.Close()
	now := time.Now()
	f.Forward("p1", Entry{ID: 1, Timestamp: now, Stream: StreamStdout, Line: "level=info started"})
	f.Forward("p1", Entry{ID: 2, Timestamp: now, Stream: StreamStderr, Line: `{"level":"error"} boom`})

	deadline := time.Now().Add(2 * time.Second)
	for f.Exported() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if f.Exported() < 2 {
		t.Fatalf("expected 2 records exported, got %d (bodies=%d)", f.Exported(), len(bodies))
	}

	mu.Lock()
	first := bodies[0]
	mu.Unlock()
	var payload struct {
		ResourceLogs []struct {
			ScopeLogs []struct {
				LogRecords []map[string]any `json:"logRecords"`
			} `json:"scopeLogs"`
		} `json:"resourceLogs"`
	}
	if err := json.Unmarshal([]byte(first), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.ResourceLogs) == 0 || len(payload.ResourceLogs[0].ScopeLogs) == 0 {
		t.Fatal("payload has no scopeLogs")
	}
	records := payload.ResourceLogs[0].ScopeLogs[0].LogRecords
	sevTexts := make(map[string]bool)
	for _, r := range records {
		sevTexts[fmt.Sprint(r["severityText"])] = true
		if body, ok := r["body"].(map[string]any); ok && body["stringValue"] == "" {
			t.Fatal("body stringValue is empty")
		}
	}
	if !sevTexts["error"] || !sevTexts["info"] {
		t.Fatalf("exported records missing expected severities: %v", sevTexts)
	}
}

// TestOTLPForwarderDropsOnSlowEndpoint proves the export queue is
// drop-on-backpressure even when the collector never answers.
func TestOTLPForwarderDropsOnSlowEndpoint(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()
	f := newOTLPForwarder(srv.URL, srv.Client(), nil, 1, 1, time.Hour)
	defer f.Close()
	defer close(block)

	for i := 0; i < 100; i++ {
		f.Forward("p", Entry{ID: uint64(i), Line: "x"})
	}
	if f.Dropped() == 0 {
		t.Fatal("expected drops when the endpoint is slow")
	}
}

// TestOTLPForwarderBestEffortNoCrash proves a dead endpoint never panics or
// wedges the pipeline: sends fail, failures are counted, and the forwarder
// keeps accepting entries.
func TestOTLPForwarderBestEffortNoCrash(t *testing.T) {
	f := newOTLPForwarder("http://127.0.0.1:1/v1/logs", &http.Client{Timeout: 100 * time.Millisecond}, nil, 2, 2, 5*time.Millisecond)
	defer f.Close()
	f.Forward("p1", Entry{ID: 1, Line: "x"})
	f.Forward("p1", Entry{ID: 2, Line: "y"})
	deadline := time.Now().Add(2 * time.Second)
	for f.Failed() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if f.Failed() == 0 {
		t.Fatal("expected failed exports to be counted")
	}
	// The forwarder is still usable after failures.
	f.Forward("p1", Entry{ID: 3, Line: "z"})
}
