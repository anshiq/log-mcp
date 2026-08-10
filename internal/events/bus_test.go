package events

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestPublishSubscribe(t *testing.T) {
	bus := New()
	ch, unsub := bus.Subscribe()
	defer unsub()
	bus.Publish(Event{Type: Started, ProcessID: "p1"})
	select {
	case e := <-ch:
		if e.Type != Started || e.ProcessID != "p1" {
			t.Fatalf("bad event: %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestSlowSubscriberDoesNotBlockPublisher(t *testing.T) {
	bus := New()
	// Fill a subscriber's buffer completely.
	ch, unsub := bus.Subscribe()
	defer unsub()
	for i := 0; i < 64; i++ {
		bus.Publish(Event{Type: Started})
	}
	// The 65th publish must not block.
	done := make(chan struct{})
	go func() {
		bus.Publish(Event{Type: Exited})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publish blocked on slow subscriber")
	}
	// Drain to prove delivery still works for consumed events.
	for i := 0; i < 64; i++ {
		<-ch
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	bus := New()
	ch, unsub := bus.Subscribe()
	unsub()
	bus.Publish(Event{Type: Started})
	select {
	case e, ok := <-ch:
		if ok {
			t.Fatalf("received event after unsubscribe: %+v", e)
		}
	case <-time.After(200 * time.Millisecond):
		// ok - channel closed
	}
}

func TestConcurrentPublish(t *testing.T) {
	bus := New()
	_, unsub := bus.Subscribe()
	defer unsub()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				bus.Publish(Event{Type: Stdout, ProcessID: "p"})
			}
		}()
	}
	wg.Wait()
}

// TestPublishAssignsMonotonicIDs verifies every published event carries a
// non-empty, monotonically increasing per-bus sequence ID.
func TestPublishAssignsMonotonicIDs(t *testing.T) {
	bus := New()
	ch, unsub := bus.Subscribe()
	defer unsub()
	for i := 0; i < 10; i++ {
		bus.Publish(Event{Type: Started})
	}
	var prev uint64
	for i := 0; i < 10; i++ {
		select {
		case e := <-ch:
			id, err := strconv.ParseUint(e.ID, 10, 64)
			if err != nil {
				t.Fatalf("bad event ID %q: %v", e.ID, err)
			}
			if id <= prev {
				t.Fatalf("event IDs not strictly increasing: %d then %d", prev, id)
			}
			prev = id
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for event")
		}
	}
}
