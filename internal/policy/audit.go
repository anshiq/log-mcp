package policy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// defaultAuditMaxBytes caps a single audit log file before it is rotated.
	defaultAuditMaxBytes = 10 * 1024 * 1024
	// defaultAuditMaxFiles is the total number of audit files kept
	// (audit.log plus audit.log.1 .. audit.log.N-1).
	defaultAuditMaxFiles = 5
	// defaultAuditQueue bounds the buffered channel between Record and the
	// writer goroutine.
	defaultAuditQueue = 1024
	// defaultQueryLines is the Query default when lines <= 0.
	defaultQueryLines = 1000
)

// auditSecretKeyPattern matches argument keys whose values must be redacted in
// audit entries. It mirrors the env redaction pattern in internal/config.
var auditSecretKeyPattern = regexp.MustCompile(`(?i)(secret|token|password|passwd|credential|api[_-]?key|access[_-]?key|private[_-]?key|auth[_-]?token)`)

// AuditEntry is one JSONL record in the audit log.
type AuditEntry struct {
	// Time is the moment the audited operation completed (UTC).
	Time time.Time `json:"time"`
	// Tool is the MCP tool (or CLI command) name, e.g. "start_process".
	Tool string `json:"tool"`
	// Args is the tool's arguments with secret-like values redacted.
	Args map[string]any `json:"args,omitempty"`
	// Result is a short outcome, e.g. "ok", "denied", or an error string.
	Result string `json:"result"`
	// DurationMS is the wall-clock duration of the operation in milliseconds.
	DurationMS int64 `json:"duration_ms"`
	// Caller identifies the caller, e.g. "mcp" or "cli".
	Caller string `json:"caller"`
}

// Audit is a non-blocking, size-rotated JSONL audit log written under the
// project's .agent-runtime directory. Record enqueues onto a bounded channel
// and never blocks the caller on disk I/O; a single writer goroutine appends
// and rotates. Any write failure degrades gracefully: the audit warns once and
// disables further writes, never taking the daemon down. Reads (Query) go
// straight to the files and are always bounded.
type Audit struct {
	dir      string
	path     string // path of the current audit.log
	logger   *slog.Logger
	maxBytes int64
	maxFiles int

	ch        chan []byte
	stopCh    chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
	closed    chan struct{}

	// mu serializes rotation against Query: writers take the lock only while
	// rotating (renaming files), queries hold it for the whole read so they
	// never observe a half-rotated set of files.
	mu sync.RWMutex
	// fh is the open handle of the current audit.log. It is owned by the
	// writer goroutine and only touched there.
	fh *os.File

	disabled atomic.Bool
	dropped  atomic.Uint64
}

// NewAudit opens (creating if necessary) the audit log at dir/audit.log and
// starts the async writer. The error path is setup-only: an un-creatable
// directory fails here so the runtime can warn and run without an audit,
// rather than crashing on first write.
func NewAudit(dir string, logger *slog.Logger) (*Audit, error) {
	return newAudit(dir, logger, defaultAuditMaxBytes, defaultAuditMaxFiles)
}

// newAudit is NewAudit with tunable rotation knobs, used by tests.
func newAudit(dir string, logger *slog.Logger, maxBytes int64, maxFiles int) (*Audit, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if maxBytes <= 0 {
		maxBytes = defaultAuditMaxBytes
	}
	if maxFiles <= 0 {
		maxFiles = defaultAuditMaxFiles
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("audit: mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, "audit.log")
	fh, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("audit: open %s: %w", path, err)
	}
	a := &Audit{
		dir:      dir,
		path:     path,
		logger:   logger,
		maxBytes: maxBytes,
		maxFiles: maxFiles,
		ch:       make(chan []byte, defaultAuditQueue),
		stopCh:   make(chan struct{}),
		closed:   make(chan struct{}),
		fh:       fh,
	}
	a.wg.Add(1)
	go a.writerLoop()
	return a, nil
}

// Record enqueues one audit entry for asynchronous persistence. It never
// blocks: when the queue is full the entry is dropped (and warned once) so
// callers are never stalled by disk back-pressure. Secret-like argument values
// are redacted before the entry is written. After the audit has been disabled
// by a write failure or closed, Record is a no-op.
func (a *Audit) Record(tool string, args map[string]any, result string, dur time.Duration, caller string) {
	if a.disabled.Load() {
		return
	}
	entry := AuditEntry{
		Time:       time.Now().UTC(),
		Tool:       tool,
		Result:     result,
		DurationMS: dur.Milliseconds(),
		Caller:     caller,
	}
	if args != nil {
		entry.Args = redactArgs(args).(map[string]any)
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return // values that cannot marshal are dropped, never fatal
	}
	select {
	case a.ch <- data:
	default:
		if a.dropped.Add(1) == 1 {
			a.logger.Warn("audit: dropping records; queue full", "dropped", a.dropped.Load())
		}
	}
}

// Query returns up to lines audit entries matching contains, newest-last by
// time. lines <= 0 means a large default. contains filters against the raw
// JSON line (tool, args, result, caller all match). Entries are read from all
// rotated files plus the current one; files are read under the rotation lock
// so a concurrent rotation never yields a torn view. Partial lines still being
// written are skipped. Query works after Close.
func (a *Audit) Query(lines int, contains string) ([]AuditEntry, error) {
	if lines <= 0 {
		lines = defaultQueryLines
	}
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Oldest to newest: audit.log.<maxFiles-1> .. audit.log.1, audit.log.
	var files []string
	for i := a.maxFiles - 1; i > 0; i-- {
		files = append(files, a.rotatedPath(i))
	}
	files = append(files, a.path)

	var out []AuditEntry
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue // absent rotated file or transient read error: skip
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if contains != "" && !strings.Contains(line, contains) {
				continue
			}
			var e AuditEntry
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				continue // partial line mid-write: skip rather than fail
			}
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	if len(out) > lines {
		out = out[len(out)-lines:]
	}
	return out, nil
}

// Close stops the writer goroutine, flushes whatever remains queued and closes
// the file. Safe to call multiple times; subsequent calls return the first
// error. The log files themselves stay on disk and remain queryable.
func (a *Audit) Close() error {
	var err error
	a.closeOnce.Do(func() {
		close(a.stopCh)
		a.wg.Wait()
		a.mu.Lock()
		if a.fh != nil {
			err = a.fh.Close()
			a.fh = nil
		}
		a.mu.Unlock()
		close(a.closed)
	})
	return err
}

// writerLoop drains the queue, appending each line to the current file and
// rotating when it grows past maxBytes. On a write failure it disables the
// audit and exits; Close then returns promptly because the writer has already
// stopped.
func (a *Audit) writerLoop() {
	defer a.wg.Done()
	for {
		select {
		case data := <-a.ch:
			if err := a.writeLine(data); err != nil {
				a.disable(err)
				return
			}
		case <-a.stopCh:
			// Graceful stop: drain whatever is still queued, then exit.
			for {
				select {
				case data := <-a.ch:
					if err := a.writeLine(data); err != nil {
						a.disable(err)
						return
					}
				default:
					return
				}
			}
		}
	}
}

// writeLine appends one marshaled entry plus a newline, rotating first if the
// current file has reached maxBytes. Only the writer goroutine calls it. The
// rotation lock is held so Query never sees a half-rotated file set.
func (a *Audit) writeLine(data []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.fh == nil {
		return fmt.Errorf("audit: file closed")
	}
	line := append(data, '\n')
	if _, err := a.fh.Write(line); err != nil {
		return fmt.Errorf("audit: append: %w", err)
	}
	if fi, err := a.fh.Stat(); err == nil && fi.Size() >= a.maxBytes {
		a.rotateLocked()
	}
	return nil
}

// rotateLocked shifts audit.log -> .1, .1 -> .2, ... .N-2 -> .N-1, drops the
// oldest (.N-1) and opens a fresh audit.log. Callers must hold a.mu.
func (a *Audit) rotateLocked() {
	os.Remove(a.rotatedPath(a.maxFiles - 1)) // drop the oldest file
	for i := a.maxFiles - 1; i > 0; i-- {
		os.Rename(a.rotatedPath(i-1), a.rotatedPath(i))
	}
	if a.fh != nil {
		a.fh.Close()
		a.fh = nil
	}
	fh, err := os.OpenFile(a.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		a.logger.Warn("audit: rotation reopen failed", "path", a.path, "error", err)
		return // fh stays nil; the next writeLine disables the audit
	}
	a.fh = fh
}

// rotatedPath returns the path of the i-th audit file; i==0 is the current
// audit.log.
func (a *Audit) rotatedPath(i int) string {
	if i == 0 {
		return a.path
	}
	return fmt.Sprintf("%s.%d", a.path, i)
}

// disable marks the audit broken: the first failure warns, subsequent writes
// and enqueues are dropped silently.
func (a *Audit) disable(err error) {
	if a.disabled.CompareAndSwap(false, true) {
		a.logger.Warn("audit: disabled", "error", err)
	}
}

// redactArgs recursively replaces the values of secret-like keys with "***" so
// audit entries never persist tokens, passwords or API keys. Non-secret
// strings and structures pass through unchanged.
func redactArgs(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if auditSecretKeyPattern.MatchString(k) {
				out[k] = "***"
				continue
			}
			out[k] = redactArgs(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = redactArgs(e)
		}
		return out
	default:
		return v
	}
}
