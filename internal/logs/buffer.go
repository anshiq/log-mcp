package logs

import (
	"sync"
)

// ringBuffer is a fixed-capacity, thread-safe FIFO of entries.
//
// Entries are appended in ID order. When the buffer is full, the oldest entry
// is evicted. It never grows beyond capacity.
type ringBuffer struct {
	mu       sync.RWMutex
	capacity int
	items    []Entry
	start    int // index of the oldest entry within items
	size     int // number of valid entries
}

func newRingBuffer(capacity int) *ringBuffer {
	if capacity < 1 {
		capacity = 1
	}
	return &ringBuffer{capacity: capacity, items: make([]Entry, capacity)}
}

// insert appends an entry, evicting the oldest when full. Caller must hold the
// ProcessLogs lock; entries must arrive in ascending ID order.
func (b *ringBuffer) insert(e Entry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.size == b.capacity {
		// evict oldest
		b.start = (b.start + 1) % b.capacity
		b.size--
	}
	idx := (b.start + b.size) % b.capacity
	b.items[idx] = e
	b.size++
}

func (b *ringBuffer) clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.start = 0
	b.size = 0
}

// entries returns all entries in chronological order. Caller must hold the
// ProcessLogs read lock.
func (b *ringBuffer) entries() []Entry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Entry, 0, b.size)
	for i := 0; i < b.size; i++ {
		out = append(out, b.items[(b.start+i)%b.capacity])
	}
	return out
}
