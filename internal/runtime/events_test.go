package runtime_test

import (
	"sync"
	"testing"
	"time"

	"agent-runtime/internal/events"
	"agent-runtime/internal/runtime"
	"agent-runtime/pkg/api"
)

func newSubManager(t *testing.T, bus *events.Bus) *runtime.SubscriptionManager {
	t.Helper()
	if bus == nil {
		bus = events.New()
	}
	m := runtime.NewSubscriptionManager(bus)
	t.Cleanup(m.Close)
	return m
}

// drainUntil pulls events from a subscription until at least min have been
// seen (draining between polls), returning them in delivery order. It fails
// the test if the pump does not deliver within a second.
func drainUntil(t *testing.T, m *runtime.SubscriptionManager, id string, min int) []api.EventDTO {
	t.Helper()
	var got []api.EventDTO
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && len(got) < min {
		ev, _, err := m.Get(id, 0)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, ev...)
		time.Sleep(time.Millisecond)
	}
	return got
}

func containsEvent(ev []api.EventDTO, typ events.Type) bool {
	for _, e := range ev {
		if e.Type == string(typ) {
			return true
		}
	}
	return false
}

func TestSubscriptionReceivesEventWithin100ms(t *testing.T) {
	bus := events.New()
	m := newSubManager(t, bus)

	id, err := m.Subscribe("c1", api.SubscribeEventsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	bus.Publish(events.Event{Type: events.Started, ProcessID: "p1", Timestamp: time.Now()})

	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		ev, _, err := m.Get(id, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(ev) == 1 {
			if ev[0].ProcessID != "p1" || ev[0].Type != string(events.Started) || ev[0].ID == "" {
				t.Fatalf("bad event: %+v", ev[0])
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("event not delivered within 100ms")
}

// TestEventSlowSubscriberNeverBlocksPublisher floods the bus while one subscription
// is never drained. The publisher must never block, and a healthy subscriber
// must keep receiving events. Run under -race this also exercises the pump's
// concurrent buffer writes.
func TestEventSlowSubscriberNeverBlocksPublisher(t *testing.T) {
	bus := events.New()
	m := newSubManager(t, bus)

	if _, err := m.Subscribe("c1", api.SubscribeEventsRequest{}); err != nil {
		t.Fatal(err)
	}
	healthy, err := m.Subscribe("c2", api.SubscribeEventsRequest{})
	if err != nil {
		t.Fatal(err)
	}

	const (
		publishers = 4
		perPub     = 500
	)
	var wg sync.WaitGroup
	done := make(chan struct{})
	for i := 0; i < publishers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perPub; j++ {
				bus.Publish(events.Event{Type: events.Stdout, ProcessID: "p"})
			}
		}()
	}
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("publisher blocked on a slow subscriber")
	}

	ev := drainUntil(t, m, healthy, 1)
	if len(ev) == 0 {
		t.Fatal("healthy subscriber starved by wedged subscriber")
	}
}

func TestEventRingBufferDropsWhenFull(t *testing.T) {
	bus := events.New()
	m := newSubManager(t, bus)

	wedged, err := m.Subscribe("c1", api.SubscribeEventsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	probe, err := m.Subscribe("c2", api.SubscribeEventsRequest{})
	if err != nil {
		t.Fatal(err)
	}

	const filler = 300
	// Pace the publishes so the pump (bus channel buffer is 64) can keep up;
	// the drop counter must reflect the per-subscription ring, not the bus.
	for i := 0; i < filler; i++ {
		bus.Publish(events.Event{Type: events.Stdout, ProcessID: "p"})
		time.Sleep(time.Millisecond)
	}
	// Sentinel: once the probe observes it, the pump has distributed every
	// prior event (the bus channel is FIFO), so the wedged ring is settled.
	bus.Publish(events.Event{Type: events.Healthy, ProcessID: "p"})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		ev, _, err := m.Get(probe, 0)
		if err != nil {
			t.Fatal(err)
		}
		if containsEvent(ev, events.Healthy) {
			break
		}
		time.Sleep(time.Millisecond)
	}

	ev, dropped, err := m.Get(wedged, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != runtime.EventRingCapacity {
		t.Fatalf("ring should hold exactly %d events, got %d", runtime.EventRingCapacity, len(ev))
	}
	// filler + the sentinel all reached the wedged ring; only the capacity
	// survived and the overflow was counted.
	want := filler + 1 - runtime.EventRingCapacity
	if dropped != want {
		t.Fatalf("dropped = %d, want %d", dropped, want)
	}

	st := m.Stats()
	if st.Subscriptions != 2 {
		t.Fatalf("stats subscriptions = %d, want 2", st.Subscriptions)
	}
	if st.Dropped < want {
		t.Fatalf("stats dropped = %d, want at least %d", st.Dropped, want)
	}
}

func TestSubscriptionCapPerClient(t *testing.T) {
	m := newSubManager(t, events.New())

	var ids []string
	for i := 0; i < runtime.MaxSubsPerClient; i++ {
		id, err := m.Subscribe("c1", api.SubscribeEventsRequest{})
		if err != nil {
			t.Fatalf("subscribe %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	if _, err := m.Subscribe("c1", api.SubscribeEventsRequest{}); err != runtime.ErrSubLimit {
		t.Fatalf("over-limit subscribe: got %v, want ErrSubLimit", err)
	}
	if _, err := m.Subscribe("c2", api.SubscribeEventsRequest{}); err != nil {
		t.Fatalf("other client should be unaffected: %v", err)
	}
	if err := m.Unsubscribe("c2", ids[0]); err == nil {
		t.Fatal("unsubscribe by non-owner should fail")
	}
	if err := m.Unsubscribe("c1", ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Subscribe("c1", api.SubscribeEventsRequest{}); err != nil {
		t.Fatalf("subscribe after freeing a slot: %v", err)
	}
}

func TestCloseClientReleasesSubscriptions(t *testing.T) {
	m := newSubManager(t, events.New())

	var ids []string
	for i := 0; i < runtime.MaxSubsPerClient; i++ {
		id, err := m.Subscribe("c1", api.SubscribeEventsRequest{})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	m.CloseClient("c1")
	if _, _, err := m.Get(ids[0], 0); err != runtime.ErrUnknownSubscription {
		t.Fatalf("Get after CloseClient: got %v, want ErrUnknownSubscription", err)
	}
	if _, err := m.Subscribe("c1", api.SubscribeEventsRequest{}); err != nil {
		t.Fatalf("re-subscribe after CloseClient: %v", err)
	}
}

func TestEventFilteringByTypeAndProcess(t *testing.T) {
	bus := events.New()
	m := newSubManager(t, bus)

	sub, err := m.Subscribe("c1", api.SubscribeEventsRequest{
		ProcessID: "p1",
		Types:     []string{string(events.Started), string(events.Restarted)},
	})
	if err != nil {
		t.Fatal(err)
	}
	probe, err := m.Subscribe("c2", api.SubscribeEventsRequest{})
	if err != nil {
		t.Fatal(err)
	}

	bus.Publish(events.Event{Type: events.Stdout, ProcessID: "p1"})
	bus.Publish(events.Event{Type: events.Started, ProcessID: "p1"})
	bus.Publish(events.Event{Type: events.Exited, ProcessID: "p1"})
	bus.Publish(events.Event{Type: events.Restarted, ProcessID: "p1"})
	bus.Publish(events.Event{Type: events.Started, ProcessID: "p2"})
	bus.Publish(events.Event{Type: events.Crashed, ProcessID: "p9"}) // sentinel

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		ev, _, err := m.Get(probe, 0)
		if err != nil {
			t.Fatal(err)
		}
		if containsEvent(ev, events.Crashed) {
			break
		}
		time.Sleep(time.Millisecond)
	}

	ev, _, err := m.Get(sub, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 2 {
		t.Fatalf("filtered sub got %d events, want 2: %+v", len(ev), ev)
	}
	for _, e := range ev {
		if e.ProcessID != "p1" {
			t.Fatalf("unexpected process: %+v", e)
		}
		if e.Type != string(events.Started) && e.Type != string(events.Restarted) {
			t.Fatalf("unexpected type: %+v", e)
		}
	}
}

func TestEventSinceCursorAndBackfill(t *testing.T) {
	bus := events.New()
	m := newSubManager(t, bus)

	bus.Publish(events.Event{Type: events.Started, ProcessID: "p1"})   // id 1
	bus.Publish(events.Event{Type: events.Exited, ProcessID: "p1"})    // id 2
	bus.Publish(events.Event{Type: events.Restarted, ProcessID: "p1"}) // id 3

	// since="2": backfill replays id 3 from the retained history, then live
	// delivery continues from id 4 onwards — in order.
	sub, err := m.Subscribe("c1", api.SubscribeEventsRequest{Since: "2"})
	if err != nil {
		t.Fatal(err)
	}
	bus.Publish(events.Event{Type: events.Healthy, ProcessID: "p1"}) // id 4

	got := drainUntil(t, m, sub, 2)
	if len(got) != 2 {
		t.Fatalf("since backfill got %d events, want 2: %+v", len(got), got)
	}
	if got[0].Type != string(events.Restarted) || got[1].Type != string(events.Healthy) {
		t.Fatalf("since backfill delivered out of order: %+v", got)
	}

	// "last" and empty start fresh from now: no backfill, only live events.
	for _, since := range []string{"last", ""} {
		sub2, err := m.Subscribe("c1", api.SubscribeEventsRequest{Since: since})
		if err != nil {
			t.Fatal(err)
		}
		bus.Publish(events.Event{Type: events.Crashed, ProcessID: "p1"})
		got2 := drainUntil(t, m, sub2, 1)
		if len(got2) != 1 || got2[0].Type != string(events.Crashed) {
			t.Fatalf("since=%q got %+v, want only the post-subscribe crash", since, got2)
		}
	}

	if _, err := m.Subscribe("c1", api.SubscribeEventsRequest{Since: "banana"}); err == nil {
		t.Fatal("invalid since should error")
	}
}

func TestEventGetLimitDrainsOldestFirst(t *testing.T) {
	bus := events.New()
	m := newSubManager(t, bus)

	sub, err := m.Subscribe("c1", api.SubscribeEventsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	probe, err := m.Subscribe("c2", api.SubscribeEventsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		bus.Publish(events.Event{Type: events.Stdout, ProcessID: "p"})
	}
	// The probe seeing all 5 proves the pump distributed them, so sub's buffer
	// holds all 5 untouched.
	drainUntil(t, m, probe, 5)

	ev, dropped, err := m.Get(sub, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 2 || dropped != 0 {
		t.Fatalf("partial drain: got %d events, dropped %d", len(ev), dropped)
	}
	// The remaining three are still buffered and delivered on the next Get.
	rest := drainUntil(t, m, sub, 3)
	if len(rest) != 3 {
		t.Fatalf("remaining drain got %d events, want 3", len(rest))
	}
}
