package events

import (
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Type is the kind of lifecycle or data event.
type Type string

const (
	Started   Type = "process.started"
	Stdout    Type = "process.stdout"
	Stderr    Type = "process.stderr"
	Exited    Type = "process.exited"
	Stopped   Type = "process.stopped"
	Failed    Type = "process.failed"
	Restarted Type = "process.restarted"
	Healthy   Type = "process.healthy"
	Unhealthy Type = "process.unhealthy"
	Crashed   Type = "process.crashed"
)

// Event is a single runtime event.
type Event struct {
	// ID is a monotonic per-bus sequence number assigned by Publish. It makes
	// events individually addressable (a future get_events tool can page by
	// ID); it is empty only for events that were never published.
	ID         string
	Type       Type
	ProcessID  string
	InstanceID string
	Timestamp  time.Time
	Payload    any
}

// Bus is a small non-blocking publish/subscribe hub. Subscribers that cannot
// keep up have events dropped rather than blocking producers. Log data is NOT
// routed through here; the per-process log buffers are the source of truth.
type Bus struct {
	mu   sync.RWMutex
	seq  atomic.Uint64
	subs map[chan Event]struct{}
}

func New() *Bus {
	return &Bus{subs: make(map[chan Event]struct{})}
}

// Publish fans an event out to all subscribers without blocking. Slow or
// absent subscribers simply miss this event. Each event is stamped with the
// bus's next monotonic sequence ID.
func (b *Bus) Publish(e Event) {
	e.ID = strconv.FormatUint(b.seq.Add(1), 10)
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Subscribe returns a receive-only channel and an unsubscribe function. The
// channel is buffered (size 64) and never blocks publishers.
func (b *Bus) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		close(ch)
		b.mu.Unlock()
	}
}
