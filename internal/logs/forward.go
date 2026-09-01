package logs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrNoPrimary is returned by TeeSink.Query and TeeSink.DeleteProcess when the
// tee has no primary sink to delegate to.
var ErrNoPrimary = errors.New("logs: tee sink has no primary sink")

// Forwarder receives every appended log entry. Forward must never block the
// caller: implementations enqueue onto a bounded queue and drop (counting the
// drop) when the queue is full, exactly like the SQLite archive's enqueue
// path.
type Forwarder interface {
	Forward(processID string, e Entry)
}

// Dropper is implemented by Forwarders that drop entries under back-pressure.
// Use TeeSink.ForwardDropped to aggregate across a fan-out.
type Dropper interface {
	Dropped() uint64
}

// TeeSink is a logs.Sink that delegates the full Sink interface to a primary
// sink (which may be nil) and additionally fans every appended entry out to
// zero or more Forwarders. Forwarding is best-effort and drop-on-backpressure:
// a slow or failing forwarder never delays the primary sink or the caller.
type TeeSink struct {
	Sink     Sink
	Forwards []Forwarder
}

// NewTee wraps primary with the given forwarders. primary may be nil (memory
// mode with log forwarding enabled): Appends are still fanned out.
func NewTee(primary Sink, f ...Forwarder) *TeeSink {
	return &TeeSink{Sink: primary, Forwards: f}
}

// Append records the entry on the primary sink and fans it out to every
// forwarder. It never blocks: the primary must be an asynchronous Sink and
// every Forwarder must be drop-on-backpressure.
func (t *TeeSink) Append(processID string, e Entry) {
	if t.Sink != nil {
		t.Sink.Append(processID, e)
	}
	for _, f := range t.Forwards {
		f.Forward(processID, e)
	}
}

func (t *TeeSink) Query(processID string, q Query) ([]Entry, error) {
	if t.Sink == nil {
		return nil, ErrNoPrimary
	}
	return t.Sink.Query(processID, q)
}

func (t *TeeSink) DeleteProcess(processID string) error {
	if t.Sink == nil {
		return ErrNoPrimary
	}
	return t.Sink.DeleteProcess(processID)
}

func (t *TeeSink) RecordInstance(procID, instanceID, command, workdir, profile string, startedAt int64, pid int64, exitedAt, exitCode *int64) error {
	if t.Sink == nil {
		return nil
	}
	return t.Sink.RecordInstance(procID, instanceID, command, workdir, profile, startedAt, pid, exitedAt, exitCode)
}

// OpenInstances implements logs.InstanceArchive by delegating to the primary
// sink when it supports it, so adoption works through a TeeSink wrapping an
// SQLite archive. With no primary (memory mode + forwarding) there is nothing
// to recover.
func (t *TeeSink) OpenInstances() ([]InstanceRecord, error) {
	if t.Sink == nil {
		return nil, nil
	}
	if ar, ok := t.Sink.(InstanceArchive); ok {
		return ar.OpenInstances()
	}
	return nil, nil
}

// ForwardDropped returns the total number of entries dropped by all
// forwarders.
func (t *TeeSink) ForwardDropped() uint64 {
	var n uint64
	for _, f := range t.Forwards {
		if d, ok := f.(Dropper); ok {
			n += d.Dropped()
		}
	}
	return n
}

// Close closes the primary sink and then every forwarder that implements
// io.Closer, so background forwarder goroutines do not leak. Safe to call
// multiple times.
func (t *TeeSink) Close() error {
	var first error
	if t.Sink != nil {
		if err := t.Sink.Close(); err != nil {
			first = err
		}
	}
	for _, f := range t.Forwards {
		if c, ok := f.(io.Closer); ok {
			if err := c.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

// forwardItem is one unit of forwarded work.
type forwardItem struct {
	processID string
	e         Entry
}

// forwardWorker is a bounded queue drained by a single worker goroutine. It
// provides the drop-on-backpressure contract: forward enqueues non-blocking
// and counts the entry as dropped when the queue is full. The worker is
// started lazily on the first forward and stopped by close.
type forwardWorker struct {
	name    string
	queue   chan forwardItem
	handle  func(processID string, e Entry)
	logger  *slog.Logger
	dropMsg string

	startOnce sync.Once
	closeOnce sync.Once
	stopCh    chan struct{}
	doneCh    chan struct{}
	dropped   atomic.Uint64
}

func newForwardWorker(name string, queueSize int, logger *slog.Logger, dropMsg string, handle func(processID string, e Entry)) *forwardWorker {
	if queueSize <= 0 {
		queueSize = 256
	}
	return &forwardWorker{
		name:    name,
		queue:   make(chan forwardItem, queueSize),
		handle:  handle,
		logger:  logger,
		dropMsg: dropMsg,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
}

func (w *forwardWorker) forward(processID string, e Entry) {
	w.startOnce.Do(func() { go w.loop() })
	select {
	case w.queue <- forwardItem{processID: processID, e: e}:
	default:
		n := w.dropped.Add(1)
		if n == 1 && w.logger != nil {
			w.logger.Warn(w.dropMsg, "forwarder", w.name, "dropped", n)
		}
	}
}

func (w *forwardWorker) loop() {
	defer close(w.doneCh)
	for {
		select {
		case item := <-w.queue:
			w.handle(item.processID, item.e)
		case <-w.stopCh:
			// Drain whatever is still queued so Close does not lose buffered
			// lines. The handler is fast by contract (stdout printing); a slow
			// handler only delays close, never Append.
			for {
				select {
				case item := <-w.queue:
					w.handle(item.processID, item.e)
				default:
					return
				}
			}
		}
	}
}

func (w *forwardWorker) close() {
	w.closeOnce.Do(func() {
		w.startOnce.Do(func() { go w.loop() })
		close(w.stopCh)
		<-w.doneCh
	})
}

// StdoutForwarder prints entries to an io.Writer (os.Stdout by default), one
// line per entry, for runtime.log_forward.stdout. It never blocks the log
// pipeline: entries are queued on a bounded queue and printed by a background
// goroutine; overflow is dropped and counted.
type StdoutForwarder struct {
	w *forwardWorker
}

// NewStdoutForwarder creates a forwarder that prints to os.Stdout.
func NewStdoutForwarder() *StdoutForwarder {
	return NewStdoutForwarderTo(os.Stdout)
}

// NewStdoutForwarderTo creates a forwarder printing to w (used by tests).
func NewStdoutForwarderTo(w io.Writer) *StdoutForwarder {
	return &StdoutForwarder{w: newForwardWorker(
		"stdout", 512, nil,
		"log forward: dropping entries; stdout queue full",
		func(_ string, e Entry) {
			fmt.Fprintf(w, "%s %s %s\n", e.Timestamp.Format(time.RFC3339Nano), e.Stream, e.Line)
		},
	)}
}

// Forward queues an entry for printing, dropping and counting it when the
// queue is full.
func (f *StdoutForwarder) Forward(processID string, e Entry) { f.w.forward(processID, e) }

// Dropped reports how many entries were dropped because the stdout queue was
// full.
func (f *StdoutForwarder) Dropped() uint64 { return f.w.dropped.Load() }

// Close stops the background printer.
func (f *StdoutForwarder) Close() error { f.w.close(); return nil }

// OTLPForwarder batches log entries and exports them to an OpenTelemetry
// collector via the OTLP/HTTP (JSON) protocol using only the standard library.
//
// Delivery is best-effort and NOT guaranteed: entries are queued on a bounded
// queue and a single worker batches them, but a full queue, an unreachable
// collector or an HTTP failure drops the affected entries (counted) rather
// than stalling the log pipeline. Do not rely on it for audit-grade transport.
type OTLPForwarder struct {
	endpoint  string
	client    *http.Client
	logger    *slog.Logger
	queue     chan forwardItem
	batchSize int
	interval  time.Duration

	startOnce sync.Once
	closeOnce sync.Once
	stopCh    chan struct{}
	doneCh    chan struct{}
	dropped   atomic.Uint64
	failed    atomic.Uint64
	exported  atomic.Uint64
	warned    atomic.Bool
}

const (
	defaultOTLPQueueSize   = 512
	defaultOTLPBatchSize   = 100
	defaultOTLPFlushEvery  = 200 * time.Millisecond
	defaultOTLPSendTimeout = 5 * time.Second
)

// NewOTLPForwarder creates a forwarder that exports to the OTLP/HTTP logs
// endpoint at host, e.g. "http://localhost:4318/v1/logs". A missing scheme is
// defaulted to http and a missing path to /v1/logs. logger may be nil; the
// client is a default http.Client with a 5s timeout.
func NewOTLPForwarder(host string, logger *slog.Logger) *OTLPForwarder {
	return newOTLPForwarder(host, nil, logger, defaultOTLPQueueSize, defaultOTLPBatchSize, defaultOTLPFlushEvery)
}

// newOTLPForwarder is NewOTLPForwarder with tunable batching, used by tests.
func newOTLPForwarder(host string, client *http.Client, logger *slog.Logger, queueSize, batchSize int, interval time.Duration) *OTLPForwarder {
	if client == nil {
		client = &http.Client{Timeout: defaultOTLPSendTimeout}
	}
	if queueSize <= 0 {
		queueSize = defaultOTLPQueueSize
	}
	if batchSize <= 0 {
		batchSize = defaultOTLPBatchSize
	}
	if interval <= 0 {
		interval = defaultOTLPFlushEvery
	}
	return &OTLPForwarder{
		endpoint:  normalizeOTLPEndpoint(host),
		client:    client,
		logger:    logger,
		queue:     make(chan forwardItem, queueSize),
		batchSize: batchSize,
		interval:  interval,
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

// Forward queues an entry for export, dropping and counting it when the queue
// is full.
func (f *OTLPForwarder) Forward(processID string, e Entry) {
	f.startOnce.Do(func() { go f.loop() })
	select {
	case f.queue <- forwardItem{processID: processID, e: e}:
	default:
		n := f.dropped.Add(1)
		if n == 1 && f.logger != nil {
			f.logger.Warn("log forward: dropping entries; otlp queue full", "endpoint", f.endpoint, "dropped", n)
		}
	}
}

// Dropped reports how many entries were dropped because the export queue was
// full.
func (f *OTLPForwarder) Dropped() uint64 { return f.dropped.Load() }

// Failed reports how many export batches failed to reach the collector.
func (f *OTLPForwarder) Failed() uint64 { return f.failed.Load() }

// Exported reports how many entries were successfully exported.
func (f *OTLPForwarder) Exported() uint64 { return f.exported.Load() }

// Close stops the worker, flushing any partial batch, and releases its
// goroutine. Best-effort: entries still queued at close time are dropped.
func (f *OTLPForwarder) Close() error {
	f.closeOnce.Do(func() {
		f.startOnce.Do(func() { go f.loop() })
		close(f.stopCh)
		<-f.doneCh
	})
	return nil
}

func (f *OTLPForwarder) loop() {
	defer close(f.doneCh)
	ticker := time.NewTicker(f.interval)
	defer ticker.Stop()
	batch := make([]forwardItem, 0, f.batchSize)
	for {
		select {
		case item := <-f.queue:
			batch = append(batch, item)
			if len(batch) >= f.batchSize {
				f.send(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				f.send(batch)
				batch = batch[:0]
			}
		case <-f.stopCh:
			if len(batch) > 0 {
				f.send(batch)
			}
			return
		}
	}
}

// send POSTs one batch to the collector. A failure drops the whole batch
// (counted) and warns once; it never retries, so the worker can never wedge on
// a slow or broken collector beyond the client timeout.
func (f *OTLPForwarder) send(items []forwardItem) {
	if len(items) == 0 {
		return
	}
	body, err := json.Marshal(buildOTLPPayload(items))
	if err != nil {
		f.failed.Add(1)
		return
	}
	req, err := http.NewRequest(http.MethodPost, f.endpoint, bytes.NewReader(body))
	if err != nil {
		f.failOnce(err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		f.failOnce(err)
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		f.failOnce(fmt.Errorf("status %s", resp.Status))
		return
	}
	f.exported.Add(uint64(len(items)))
}

// failOnce counts a failed export and logs the first failure.
func (f *OTLPForwarder) failOnce(err error) {
	f.failed.Add(1)
	if f.warned.CompareAndSwap(false, true) && f.logger != nil {
		f.logger.Warn("log forward: otlp export failed; dropping batch (best-effort)", "endpoint", f.endpoint, "error", err)
	}
}

// normalizeOTLPEndpoint fills in a missing scheme and /v1/logs path so config
// can name a bare host or port.
func normalizeOTLPEndpoint(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return "http://localhost:4318/v1/logs"
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	if u, err := url.Parse(host); err == nil && u.Path == "" {
		host = strings.TrimRight(host, "/") + "/v1/logs"
	}
	return host
}

// otlpSeverityNumbers maps canonical levels to OTLP severity numbers.
var otlpSeverityNumbers = map[string]int{"debug": 5, "info": 9, "warn": 13, "error": 17}

// otlpLogsPayload renders a batch as the OTLP/HTTP JSON export shape
// (resourceLogs -> scopeLogs -> logRecords).
type otlpLogsPayload struct {
	ResourceLogs []otlpResourceLogs `json:"resourceLogs"`
}

type otlpResourceLogs struct {
	ScopeLogs []otlpScopeLogs `json:"scopeLogs"`
}

type otlpScopeLogs struct {
	LogRecords []otlpLogRecord `json:"logRecords"`
}

type otlpLogRecord struct {
	TimeUnixNano   uint64         `json:"timeUnixNano"`
	SeverityNumber int            `json:"severityNumber"`
	SeverityText   string         `json:"severityText"`
	Body           otlpAnyValue   `json:"body"`
	Attributes     []otlpKeyValue `json:"attributes"`
}

type otlpKeyValue struct {
	Key   string       `json:"key"`
	Value otlpAnyValue `json:"value"`
}

type otlpAnyValue struct {
	StringValue string `json:"stringValue,omitempty"`
	IntValue    int64  `json:"intValue,omitempty"`
}

func buildOTLPPayload(items []forwardItem) otlpLogsPayload {
	records := make([]otlpLogRecord, 0, len(items))
	for _, it := range items {
		sevText := ParseLevel(it.e.Line)
		sevNum := 0
		if sevText != "" {
			sevNum = otlpSeverityNumbers[sevText]
		}
		records = append(records, otlpLogRecord{
			TimeUnixNano:   uint64(it.e.Timestamp.UnixNano()),
			SeverityNumber: sevNum,
			SeverityText:   sevText,
			Body:           otlpAnyValue{StringValue: it.e.Line},
			Attributes: []otlpKeyValue{
				{Key: "process_id", Value: otlpAnyValue{StringValue: it.processID}},
				{Key: "stream", Value: otlpAnyValue{StringValue: string(it.e.Stream)}},
				{Key: "log.id", Value: otlpAnyValue{IntValue: int64(it.e.ID)}},
			},
		})
	}
	return otlpLogsPayload{
		ResourceLogs: []otlpResourceLogs{{
			ScopeLogs: []otlpScopeLogs{{LogRecords: records}},
		}},
	}
}
