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
	mu       sync.RWMutex
	nextID   uint64
	stdout   *ringBuffer
	stderr   *ringBuffer
	onAppend func()
}

// NewProcessLogs creates log buffers for one process. capacity is the number of
// lines retained per stream. onAppend, if non-nil, is invoked (outside any lock)
// after every appended entry and may be used to wake log waiters.
func NewProcessLogs(capacity int, onAppend func()) *ProcessLogs {
	return &ProcessLogs{
		nextID:   0,
		stdout:   newRingBuffer(capacity),
		stderr:   newRingBuffer(capacity),
		onAppend: onAppend,
	}
}

// Append records a line from the given stream and returns the new entry.
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
	return e
}

// Query filters and limits the log history. limit <= 0 means "all matching".
type Query struct {
	Stream   StreamFilter // default FilterAll when empty
	Lines    int          // max entries to return (tail), 0 -> all
	Contains string       // substring filter; "" disables
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

	all := p.allLocked()
	filtered := make([]Entry, 0, len(all))
	for _, e := range all {
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
	all := p.allLocked()
	for i, e := range all {
		if e.ID > id {
			return all[i:]
		}
	}
	return nil
}

// From returns all entries with ID greater than or equal to id, in ID order.
// Used to scan logs belonging to a specific instance boundary.
func (p *ProcessLogs) From(id uint64) []Entry {
	p.mu.RLock()
	defer p.mu.RUnlock()
	all := p.allLocked()
	for i, e := range all {
		if e.ID >= id {
			return all[i:]
		}
	}
	return nil
}

// NextID returns the ID the next Append will assign. Used to record instance
// log boundaries so waiters can ignore a previous instance's output.
func (p *ProcessLogs) NextID() uint64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.nextID
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
	so := p.stdout.entries()
	se := p.stderr.entries()
	merged := make([]Entry, 0, len(so)+len(se))
	merged = append(merged, so...)
	merged = append(merged, se...)
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].ID < merged[j].ID })
	return merged
}

// Store maps process IDs to their ProcessLogs buffers. Safe for concurrent use.
type Store struct {
	mu       sync.RWMutex
	capacity int
	procs    map[string]*ProcessLogs
}

func NewStore(capacity int) *Store {
	return &Store{capacity: capacity, procs: make(map[string]*ProcessLogs)}
}

// Get returns the buffers for a process, creating them on first use.
func (s *Store) Get(processID string) *ProcessLogs {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.procs[processID]; ok {
		return p
	}
	p := NewProcessLogs(s.capacity, nil)
	s.procs[processID] = p
	return p
}

// GetOrNil returns the buffers only if they already exist.
func (s *Store) GetOrNil(processID string) *ProcessLogs {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.procs[processID]
}

// Delete removes a process's buffers entirely.
func (s *Store) Delete(processID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.procs, processID)
}

// SetOnAppend registers the callback used to wake waiters for a process.
func (s *Store) SetOnAppend(processID string, fn func()) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.procs[processID]; ok {
		p.onAppend = fn
	}
}
