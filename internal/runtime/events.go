package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"agent-runtime/internal/events"
	"agent-runtime/pkg/api"
)

// Hard caps for event subscriptions (Phase 2 boundedness invariant). They are
// deliberately exported so the MCP layer can document them and the tests can
// assert against them.
const (
	// MaxSubsPerClient caps how many concurrent subscriptions one client may
	// hold. Agents are expected to hold one subscription per interest, not one
	// per request.
	MaxSubsPerClient = 8
	// EventRingCapacity is the per-subscription ring capacity. A subscriber
	// that does not call Get has events dropped (and counted) rather than
	// blocking the publisher or any other subscriber.
	EventRingCapacity = 256
	// historyCapacity bounds the shared replay log used to backfill a
	// subscriber from a `since` event-id cursor.
	historyCapacity = 256
)

// ErrSubLimit is returned by Subscribe when the client already holds
// MaxSubsPerClient subscriptions.
var ErrSubLimit = errors.New("subscription limit reached")

// ErrUnknownSubscription is returned by Get and Unsubscribe when the handle is
// not known (never created, already unsubscribed, or expired).
var ErrUnknownSubscription = errors.New("unknown subscription")

// SubscriptionManager hands out explicit, capped event subscriptions over a
// runtime's events.Bus. It reuses the bus's non-blocking model end to end: a
// single pump goroutine drains the bus and writes into per-subscription ring
// buffers, so publishers are never blocked and one wedging subscriber (one
// that never calls Get) can never stall another.
type SubscriptionManager struct {
	bus *events.Bus

	mu       sync.RWMutex
	subs     map[string]*subscription // subscriptionID -> sub
	byClient map[string]int           // client -> live subscription count
	closed   bool

	histMu  sync.Mutex
	history []events.Event // shared replay log for since-backfill (FIFO, drop-oldest at historyCapacity)

	subSeq   atomic.Uint64 // fallback subscription id source
	dropped  atomic.Uint64 // total events dropped (sub overruns + history evictions)
	busUnsub func()
}

// subscription is one client's filtered view of the event stream. The buffer
// is guarded by its own mutex so Get never contends with the manager-wide
// locks the pump takes.
type subscription struct {
	id     string
	client string

	process string               // filter: empty = all processes
	types   map[events.Type]bool // filter: nil/empty = all types
	since   uint64               // drop events with id <= since

	mu      sync.Mutex
	buf     []api.EventDTO // ring: appends until EventRingCapacity, then drops new events
	dropped uint64         // drops since the previous Get drain
	closed  bool
}

// NewSubscriptionManager wires a manager to the given bus. A nil bus is
// replaced with a fresh one so the manager is always safe to construct.
func NewSubscriptionManager(bus *events.Bus) *SubscriptionManager {
	if bus == nil {
		bus = events.New()
	}
	m := &SubscriptionManager{
		bus:      bus,
		subs:     make(map[string]*subscription),
		byClient: make(map[string]int),
	}
	ch, unsub := bus.Subscribe()
	m.busUnsub = unsub
	go m.pump(ch)
	return m
}

// pump drains the bus for the manager's lifetime and fans each event out to
// every subscription that matches. It is the only writer to per-subscription
// buffers, and it never blocks: appends are plain slice writes under a short
// mutex, and overruns are counted and skipped.
func (m *SubscriptionManager) pump(ch <-chan events.Event) {
	for e := range ch {
		m.distribute(e)
	}
}

// distribute records an event in the shared history and delivers it to every
// matching live subscription. It must not be called concurrently with itself;
// only the pump goroutine does.
func (m *SubscriptionManager) distribute(e events.Event) {
	dto := toEventDTO(e)
	// Record in the shared history first so a since-backfill always covers the
	// most recent events, evicting the oldest when the log is full.
	m.histMu.Lock()
	m.history = append(m.history, e)
	if len(m.history) > historyCapacity {
		m.history = m.history[len(m.history)-historyCapacity:]
		m.dropped.Add(1)
	}
	m.histMu.Unlock()

	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.subs {
		s.mu.Lock()
		if !s.closed && s.matches(e) {
			if len(s.buf) == EventRingCapacity {
				s.dropped++
				m.dropped.Add(1)
			} else {
				s.buf = append(s.buf, dto)
			}
		}
		s.mu.Unlock()
	}
}

// matches reports whether an event passes the subscription's process, type and
// since filters. Shared by the live delivery path and the since-backfill path.
func (s *subscription) matches(e events.Event) bool {
	if s.process != "" && e.ProcessID != s.process {
		return false
	}
	if len(s.types) > 0 && !s.types[e.Type] {
		return false
	}
	if s.since > 0 {
		if id, err := strconv.ParseUint(e.ID, 10, 64); err == nil && id <= s.since {
			return false
		}
	}
	return true
}

// Subscribe registers a new subscription for the client and returns its
// handle. The subscription is filtered by req.ProcessID and/or req.Types
// (empty means all) and starts after req.Since ("last" or empty means start
// fresh from now; a numeric event id backfills from the retained history).
// It returns ErrSubLimit when the client already holds MaxSubsPerClient
// subscriptions. Events are never pushed to the subscriber; the caller pulls
// them with Get.
func (m *SubscriptionManager) Subscribe(client string, req api.SubscribeEventsRequest) (string, error) {
	if client == "" {
		return "", errors.New("subscribe requires a client id")
	}
	types := make(map[events.Type]bool, len(req.Types))
	for _, t := range req.Types {
		if t != "" {
			types[events.Type(t)] = true
		}
	}
	var since uint64
	switch req.Since {
	case "", "last":
		// live only, no backfill
	default:
		id, err := strconv.ParseUint(req.Since, 10, 64)
		if err != nil {
			return "", fmt.Errorf("invalid since %q: want an event id, \"last\" or empty", req.Since)
		}
		since = id
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return "", errors.New("subscription manager is closed")
	}
	if m.byClient[client] >= MaxSubsPerClient {
		return "", ErrSubLimit
	}

	id := m.newID()
	sub := &subscription{
		id: id, client: client,
		process: req.ProcessID, types: types, since: since,
	}
	// Backfill from the shared history before going live so the subscription
	// is fully populated before any live event can be delivered to it (the
	// pump needs the write lock we hold, so it cannot interleave).
	if since > 0 {
		m.histMu.Lock()
		for _, h := range m.history {
			if sub.matches(h) {
				if len(sub.buf) == EventRingCapacity {
					sub.dropped++
					m.dropped.Add(1)
				} else {
					sub.buf = append(sub.buf, toEventDTO(h))
				}
			}
		}
		m.histMu.Unlock()
	}
	m.subs[id] = sub
	m.byClient[client]++
	return id, nil
}

// Get drains up to limit buffered events from a subscription, oldest first.
// A limit <= 0 drains everything currently buffered. The returned dropped
// count is the number of events dropped since the previous Get because the
// subscription's ring buffer was full; it is reset by this call. Get never
// blocks the pump or the publisher.
func (m *SubscriptionManager) Get(subscriptionID string, limit int) ([]api.EventDTO, int, error) {
	m.mu.RLock()
	sub, ok := m.subs[subscriptionID]
	m.mu.RUnlock()
	if !ok {
		return nil, 0, ErrUnknownSubscription
	}

	sub.mu.Lock()
	defer sub.mu.Unlock()
	if sub.closed {
		return nil, 0, ErrUnknownSubscription
	}
	dropped := sub.dropped
	sub.dropped = 0
	if limit <= 0 || limit >= len(sub.buf) {
		ev := sub.buf
		sub.buf = nil
		return ev, int(dropped), nil
	}
	// Partial drain: take the oldest limit events and keep the rest buffered
	// for the next call. Copy the retained tail so the returned head can never
	// be mutated by later appends to the same backing array.
	head := sub.buf[:limit]
	sub.buf = append([]api.EventDTO(nil), sub.buf[limit:]...)
	return head, int(dropped), nil
}

// Unsubscribe closes a subscription, releasing its ring buffer. The caller
// must own the subscription (the client it was created for). Unsubscribing an
// unknown handle returns ErrUnknownSubscription.
func (m *SubscriptionManager) Unsubscribe(client, subscriptionID string) error {
	m.mu.Lock()
	sub, ok := m.subs[subscriptionID]
	if !ok {
		m.mu.Unlock()
		return ErrUnknownSubscription
	}
	if sub.client != client {
		m.mu.Unlock()
		return fmt.Errorf("subscription %q belongs to another client", subscriptionID)
	}
	delete(m.subs, subscriptionID)
	m.byClient[client]--
	if m.byClient[client] <= 0 {
		delete(m.byClient, client)
	}
	sub.mu.Lock()
	sub.closed = true
	sub.buf = nil
	sub.mu.Unlock()
	m.mu.Unlock()
	return nil
}

// CloseClient auto-cleans every subscription a client holds. Call it when a
// client disconnects so its ring buffers are freed and the per-client cap is
// reset.
func (m *SubscriptionManager) CloseClient(client string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, sub := range m.subs {
		if sub.client == client {
			delete(m.subs, id)
			sub.mu.Lock()
			sub.closed = true
			sub.buf = nil
			sub.mu.Unlock()
		}
	}
	delete(m.byClient, client)
}

// Close stops the manager: it unsubscribes from the bus (ending the pump
// goroutine) and closes every live subscription. Subscribe after Close returns
// an error. It is idempotent: a second Close is a no-op, so a runtime whose
// Shutdown is called more than once cannot double-close the bus channel.
func (m *SubscriptionManager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	busUnsub := m.busUnsub
	m.busUnsub = nil
	subs := m.subs
	m.subs = make(map[string]*subscription)
	clear(m.byClient)
	m.mu.Unlock()

	if busUnsub != nil {
		busUnsub()
	}
	for _, sub := range subs {
		sub.mu.Lock()
		sub.closed = true
		sub.buf = nil
		sub.mu.Unlock()
	}
}

// Stats returns a snapshot of live subscription count and the total number of
// events dropped since the manager was created (per-subscription overruns plus
// shared-history evictions).
func (m *SubscriptionManager) Stats() api.EventStats {
	m.mu.RLock()
	n := len(m.subs)
	m.mu.RUnlock()
	return api.EventStats{Subscriptions: n, Dropped: int(m.dropped.Load())}
}

// newID returns a unique subscription handle, falling back to a sequence
// number if the system entropy source is unavailable.
func (m *SubscriptionManager) newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("sub_%d", m.subSeq.Add(1))
	}
	return "sub_" + hex.EncodeToString(b)
}

// toEventDTO converts a bus event into its wire form.
func toEventDTO(e events.Event) api.EventDTO {
	return api.EventDTO{
		ID:         e.ID,
		Type:       string(e.Type),
		ProcessID:  e.ProcessID,
		InstanceID: e.InstanceID,
		Timestamp:  e.Timestamp.Format(time.RFC3339Nano),
		Payload:    e.Payload,
	}
}
