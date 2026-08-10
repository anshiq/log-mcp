package logs

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// ProcessLogs holds the bounded stdout and stderr buffers for a single logical
// process. Entry IDs are assigned from a shared counter so stdout and stderr
// form one chronological sequence.
type ProcessLogs struct {
	mu        sync.RWMutex
	nextID    uint64
	stdout    *ringBuffer
	stderr    *ringBuffer
	onAppend  func()
	processID string
	sink      Sink
}

// NewProcessLogs creates log buffers for one process. capacity is the number of
// lines retained per stream. onAppend, if non-nil, is invoked (outside any lock)
// after every appended entry and may be used to wake log waiters.
func NewProcessLogs(capacity int, onAppend func()) *ProcessLogs {
	return NewProcessLogsWithSink(capacity, "", onAppend, nil)
}

// NewProcessLogsWithSink is NewProcessLogs with an optional durable Sink. When
// sink is non-nil, every appended entry is forwarded to it (outside any lock)
// after onAppend runs. processID is the logical process identity recorded
// alongside the entries for archive lookups.
func NewProcessLogsWithSink(capacity int, processID string, onAppend func(), sink Sink) *ProcessLogs {
	return &ProcessLogs{
		nextID:    0,
		stdout:    newRingBuffer(capacity),
		stderr:    newRingBuffer(capacity),
		onAppend:  onAppend,
		processID: processID,
		sink:      sink,
	}
}

// Append records a line from the given stream and returns the new entry.
//
// The sink, when present, is invoked after the ring buffers are updated and
// the lock is released. It must never run synchronously under p.mu: Sink
// implementations are asynchronous (they enqueue and flush on their own
// goroutines), but the ordering guarantee here is that the ring buffer is
// always updated before the sink is notified.
func (p *ProcessLogs) Append(stream Stream, line string) Entry {
	p.mu.Lock()
	e := Entry{
		ID:        p.nextID,
		Timestamp: time.Now(),
		Stream:    stream,
		Line:      line,
	}
	p.nextID++
	if stream == StreamStdout {
		p.stdout.insert(e)
	} else {
		p.stderr.insert(e)
	}
	p.mu.Unlock()
	if p.onAppend != nil {
		p.onAppend()
	}
	if p.sink != nil {
		p.sink.Append(p.processID, e)
	}
	return e
}

// Query filters and limits the log history. limit <= 0 means "all matching".
type Query struct {
	Stream   StreamFilter // default FilterAll when empty
	Lines    int          // max entries to return (tail), 0 -> all
	Contains string       // substring filter; "" disables
	BeforeID uint64       // return only entries with ID < BeforeID; 0 disables
}

// Result is the outcome of a Query.
type Result struct {
	Entries   []Entry // newest-last, limited to Lines
	Available int     // total matching entries available
	Truncated bool    // true if more matching entries existed than returned
}

// Query returns the matching tail of the log history.
func (p *ProcessLogs) Query(q Query) Result {
	p.mu.RLock()
	defer p.mu.RUnlock()

	so := p.stdout.entries()
	se := p.stderr.entries()
	// For a single-stream filter no merge is needed: each ring is already in
	// ID order and the other stream is filtered out anyway.
	var all []Entry
	switch q.Stream {
	case FilterStdout:
		all = so
	case FilterStderr:
		all = se
	default:
		all = mergeLocked(so, se)
	}
	filtered := make([]Entry, 0, len(all))
	for _, e := range all {
		if q.BeforeID > 0 && e.ID >= q.BeforeID {
			continue
		}
		switch q.Stream {
		case FilterStdout:
			if e.Stream != StreamStdout {
				continue
			}
		case FilterStderr:
			if e.Stream != StreamStderr {
				continue
			}
		}
		if q.Contains != "" && !strings.Contains(e.Line, q.Contains) {
			continue
		}
		filtered = append(filtered, e)
	}

	available := len(filtered)
	limit := q.Lines
	if limit < 1 || limit > available {
		limit = available
	}
	res := Result{Available: available, Truncated: available > limit}
	if limit > 0 {
		res.Entries = filtered[available-limit:]
	}
	return res
}

// After returns all entries with ID strictly greater than id, in ID order. Used
// by log waiters to pick up only newly appended entries.
func (p *ProcessLogs) After(id uint64) []Entry {
	p.mu.RLock()
	defer p.mu.RUnlock()
	so := p.stdout.entries()
	se := p.stderr.entries()
	// Both rings are ID-sorted, so binary-search the start of each suffix and
	// merge only that suffix.
	i := sort.Search(len(so), func(i int) bool { return so[i].ID > id })
	j := sort.Search(len(se), func(i int) bool { return se[i].ID > id })
	return mergeLocked(so[i:], se[j:])
}

// From returns all entries with ID greater than or equal to id, in ID order.
// Used to scan logs belonging to a specific instance boundary.
func (p *ProcessLogs) From(id uint64) []Entry {
	p.mu.RLock()
	defer p.mu.RUnlock()
	so := p.stdout.entries()
	se := p.stderr.entries()
	i := sort.Search(len(so), func(i int) bool { return so[i].ID >= id })
	j := sort.Search(len(se), func(i int) bool { return se[i].ID >= id })
	return mergeLocked(so[i:], se[j:])
}

// NextID returns the ID the next Append will assign. Used to record instance
// log boundaries so waiters can ignore a previous instance's output.
func (p *ProcessLogs) NextID() uint64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.nextID
}

// OldestID returns the smallest entry ID currently retained in memory across
// both streams, or 0 when no entries are retained. Used by the runtime to bound
// durable-archive back-fill queries: entries with ID < OldestID() were evicted
// from the ring buffer and may still live in the archive.
func (p *ProcessLogs) OldestID() uint64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	oldest := uint64(0)
	for _, b := range []*ringBuffer{p.stdout, p.stderr} {
		if b.size == 0 {
			continue
		}
		first := b.items[b.start].ID
		if oldest == 0 || first < oldest {
			oldest = first
		}
	}
	return oldest
}

// Count returns the number of retained entries for a stream.
func (p *ProcessLogs) Count(stream Stream) int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if stream == StreamStdout {
		return p.stdout.size
	}
	return p.stderr.size
}

// Clear empties the selected buffers and returns the number of entries removed.
func (p *ProcessLogs) Clear(stream StreamFilter) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	var n int
	if stream == FilterAll || stream == "" {
		n = p.stdout.size + p.stderr.size
		p.stdout.clear()
		p.stderr.clear()
		return n
	}
	if stream == FilterStdout {
		n = p.stdout.size
		p.stdout.clear()
		return n
	}
	n = p.stderr.size
	p.stderr.clear()
	return n
}

// Len returns the number of entries retained across both streams.
func (p *ProcessLogs) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.stdout.size + p.stderr.size
}

// allLocked returns every retained entry merged into chronological (ID) order.
// Caller must hold the read lock.
func (p *ProcessLogs) allLocked() []Entry {
	return mergeLocked(p.stdout.entries(), p.stderr.entries())
}

// mergeLocked merges two ID-ordered entry slices into one ID-ordered slice in
// O(n) without sorting. IDs are unique (assigned from a shared counter), so
// ties cannot occur. When either input is empty the other is returned as-is;
// when both are empty, nil is returned (preserving the pre-merge semantics of
// From/After).
func mergeLocked(a, b []Entry) []Entry {
	if len(a) == 0 {
		if len(b) == 0 {
			return nil
		}
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make([]Entry, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i].ID < b[j].ID {
			out = append(out, a[i])
			i++
		} else {
			out = append(out, b[j])
			j++
		}
	}
	out = append(out, a[i:]...)
	out = append(out, b[j:]...)
	return out
}

// Store maps process IDs to their ProcessLogs buffers. Safe for concurrent use.
type Store struct {
	mu       sync.RWMutex
	capacity int
	sink     Sink
	procs    map[string]*ProcessLogs
}

func NewStore(capacity int) *Store {
	return NewStoreWithSink(capacity, nil)
}

// NewStoreWithSink creates a Store whose ProcessLogs forward every appended
// entry to sink (may be nil). The sink is captured at construction and passed
// to every ProcessLogs created afterwards.
func NewStoreWithSink(capacity int, sink Sink) *Store {
	return &Store{capacity: capacity, sink: sink, procs: make(map[string]*ProcessLogs)}
}

// SetSink sets the sink forwarded to ProcessLogs created by future Get calls.
// It must be called before any process starts (i.e. before the first Get), and
// is not safe to call concurrently with Get/Append.
func (s *Store) SetSink(sink Sink) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sink = sink
}

// Get returns the buffers for a process, creating them on first use. onAppend,
// if non-nil, is wired into a newly created ProcessLogs and is invoked (outside
// any lock) after every appended entry. It is set exactly once, before the
// process is visible to readers, and is immutable thereafter; a later Get for
// the same process never modifies an existing callback.
func (s *Store) Get(processID string, onAppend func()) *ProcessLogs {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.procs[processID]; ok {
		return p
	}
	p := NewProcessLogsWithSink(s.capacity, processID, onAppend, s.sink)
	s.procs[processID] = p
	return p
}

// GetOrNil returns the buffers only if they already exist.
func (s *Store) GetOrNil(processID string) *ProcessLogs {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.procs[processID]
}

// Delete removes a process's buffers entirely and, when a sink is configured,
// asks the sink to drop the process's durable rows too. The sink call runs
// outside the store lock so concurrent readers are not blocked on DB I/O.
func (s *Store) Delete(processID string) {
	s.mu.Lock()
	delete(s.procs, processID)
	sink := s.sink
	s.mu.Unlock()
	if sink != nil {
		// A failure here only leaves orphaned archive rows; the in-memory
		// removal has already succeeded, so the error is intentionally ignored.
		_ = sink.DeleteProcess(processID)
	}
}
