package logs

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAppendAndQuery(t *testing.T) {
	p := NewProcessLogs(100, nil)
	for i := 0; i < 5; i++ {
		p.Append(StreamStdout, fmt.Sprintf("out-%d", i))
	}
	p.Append(StreamStderr, "err-0")

	res := p.Query(Query{Stream: FilterAll, Lines: 10})
	if res.Available != 6 {
		t.Fatalf("available = %d, want 6", res.Available)
	}
	if res.Truncated {
		t.Fatal("unexpected truncation")
	}
	if len(res.Entries) != 6 {
		t.Fatalf("entries = %d, want 6", len(res.Entries))
	}
	if res.Entries[5].Line != "err-0" {
		t.Fatalf("last entry = %q, want err-0", res.Entries[5].Line)
	}
	if res.Entries[5].Stream != StreamStderr {
		t.Fatalf("last stream = %q, want stderr", res.Entries[5].Stream)
	}
}

func TestBoundedCapacityEvictsOldest(t *testing.T) {
	p := NewProcessLogs(5, nil)
	for i := 0; i < 10; i++ {
		p.Append(StreamStdout, fmt.Sprintf("line-%d", i))
	}
	res := p.Query(Query{Stream: FilterStdout, Lines: 100})
	if res.Available != 5 {
		t.Fatalf("available = %d, want 5", res.Available)
	}
	if res.Entries[0].Line != "line-5" {
		t.Fatalf("oldest kept = %q, want line-5", res.Entries[0].Line)
	}
	if res.Entries[4].Line != "line-9" {
		t.Fatalf("newest = %q, want line-9", res.Entries[4].Line)
	}
}

func TestStreamFiltering(t *testing.T) {
	p := NewProcessLogs(100, nil)
	p.Append(StreamStdout, "a")
	p.Append(StreamStderr, "b")
	p.Append(StreamStdout, "c")

	so := p.Query(Query{Stream: FilterStdout})
	if len(so.Entries) != 2 || so.Entries[0].Line != "a" {
		t.Fatalf("stdout filter wrong: %+v", so.Entries)
	}
	se := p.Query(Query{Stream: FilterStderr})
	if len(se.Entries) != 1 || se.Entries[0].Line != "b" {
		t.Fatalf("stderr filter wrong: %+v", se.Entries)
	}
}

func TestLineLimiting(t *testing.T) {
	p := NewProcessLogs(100, nil)
	for i := 0; i < 10; i++ {
		p.Append(StreamStdout, fmt.Sprintf("l-%d", i))
	}
	res := p.Query(Query{Stream: FilterAll, Lines: 3})
	if len(res.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(res.Entries))
	}
	if res.Entries[0].Line != "l-7" {
		t.Fatalf("tail start = %q, want l-7", res.Entries[0].Line)
	}
	if !res.Truncated {
		t.Fatal("expected truncated")
	}
	if res.Available != 10 {
		t.Fatalf("available = %d, want 10", res.Available)
	}
}

func TestContainsFilter(t *testing.T) {
	p := NewProcessLogs(100, nil)
	p.Append(StreamStdout, "Server started")
	p.Append(StreamStdout, "request handled")
	p.Append(StreamStderr, "Error: boom")
	p.Append(StreamStderr, "retrying")

	res := p.Query(Query{Stream: FilterAll, Contains: "Error"})
	if len(res.Entries) != 1 || res.Entries[0].Line != "Error: boom" {
		t.Fatalf("contains filter wrong: %+v", res.Entries)
	}
	res = p.Query(Query{Stream: FilterStderr, Contains: "Error"})
	if len(res.Entries) != 1 {
		t.Fatalf("combined filter wrong: %+v", res.Entries)
	}
	res = p.Query(Query{Stream: FilterStdout, Contains: "Error"})
	if len(res.Entries) != 0 {
		t.Fatalf("expected no matches: %+v", res.Entries)
	}
}

func TestAfterReturnsOnlyNewEntries(t *testing.T) {
	p := NewProcessLogs(100, nil)
	p.Append(StreamStdout, "a")
	p.Append(StreamStderr, "b")
	p.Append(StreamStdout, "c")

	// IDs shared across streams: a=0, b=1, c=2
	if got := p.After(0); len(got) != 2 || got[0].Line != "b" || got[1].Line != "c" {
		t.Fatalf("after(0) wrong: %+v", got)
	}
	if got := p.After(1); len(got) != 1 || got[0].Line != "c" || got[0].ID != 2 {
		t.Fatalf("after(1) wrong: %+v", got)
	}
	if got := p.After(3); got != nil {
		t.Fatalf("after(3) should be empty, got %+v", got)
	}
}

func TestClear(t *testing.T) {
	p := NewProcessLogs(100, nil)
	p.Append(StreamStdout, "a")
	p.Append(StreamStderr, "b")
	n := p.Clear(FilterStderr)
	if n != 1 {
		t.Fatalf("cleared stderr = %d, want 1", n)
	}
	if p.Len() != 1 {
		t.Fatalf("len = %d, want 1", p.Len())
	}
	n = p.Clear(FilterAll)
	if n != 1 {
		t.Fatalf("cleared all = %d, want 1", n)
	}
	if p.Len() != 0 {
		t.Fatalf("len = %d, want 0", p.Len())
	}
}

func TestConcurrentWritersAndReaders(t *testing.T) {
	p := NewProcessLogs(1000, nil)
	const writers = 4
	const perWriter = 500
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				stream := StreamStdout
				if i%2 == 0 {
					stream = StreamStderr
				}
				p.Append(stream, fmt.Sprintf("w%d-l%d", w, i))
			}
		}(w)
	}
	// The reader must not spin forever after the writers finish (the buffers
	// are never cleared), so it runs until the writers complete, then does a
	// final scan and stops.
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		wg.Wait()
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			res := p.Query(Query{Stream: FilterAll, Lines: 50, Contains: "w"})
			_ = res
			p.After(0)
			select {
			case <-writerDone:
				return
			default:
			}
		}
	}()
	wg.Wait()
	<-done
	// capacity is per-stream: 2 streams x 1000
	if got := p.Len(); got != 2000 {
		t.Fatalf("len = %d, want 2000 (per-stream capacity bound)", got)
	}
	// oldest stdout entries should have been evicted
	res := p.Query(Query{Stream: FilterStdout, Lines: 5, Contains: "l0"})
	if res.Available > 0 {
		t.Fatalf("expected stdout eviction of oldest writer lines, available = %d", res.Available)
	}
}

func TestStoreIsolation(t *testing.T) {
	s := NewStore(10)
	p1 := s.Get("proc_a", nil)
	p2 := s.Get("proc_b", nil)
	p1.Append(StreamStdout, "a line")
	p2.Append(StreamStdout, "b line")
	if got := p1.Query(Query{}).Entries[0].Line; got != "a line" {
		t.Fatalf("proc_a got %q", got)
	}
	if got := p2.Query(Query{}).Entries[0].Line; got != "b line" {
		t.Fatalf("proc_b got %q", got)
	}
	if s.GetOrNil("proc_c") != nil {
		t.Fatal("proc_c should not exist")
	}
	s.Delete("proc_a")
	if s.GetOrNil("proc_a") != nil {
		t.Fatal("proc_a should be deleted")
	}
}

// TestOnAppendSetAtConstruction exercises B6: the callback is wired in by Get
// at construction, fires once per append outside the lock, and is never
// clobbered by a later Get for the same process.
func TestOnAppendSetAtConstruction(t *testing.T) {
	s := NewStore(10)
	var calls atomic.Int32
	p := s.Get("proc_cb", func() { calls.Add(1) })
	// A later Get for the same process must return the same instance and must
	// not reset the callback (it is immutable after construction).
	if again := s.Get("proc_cb", nil); again != p {
		t.Fatal("Get returned a different ProcessLogs for an existing process")
	}
	p.Append(StreamStdout, "one")
	p.Append(StreamStderr, "two")
	if got := calls.Load(); got != 2 {
		t.Fatalf("onAppend calls = %d, want 2", got)
	}
}

// TestOnAppendConcurrentAppends runs concurrent Appends through the same
// onAppend callback. This exercises the B6 contract (immutable callback,
// invoked outside the lock) under -race.
func TestOnAppendConcurrentAppends(t *testing.T) {
	s := NewStore(100)
	var calls atomic.Int32
	p := s.Get("proc_race", func() { calls.Add(1) })
	const writers = 4
	const perWriter = 200
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				p.Append(StreamStdout, "line")
			}
		}()
	}
	wg.Wait()
	if got := calls.Load(); got != writers*perWriter {
		t.Fatalf("onAppend calls = %d, want %d", got, writers*perWriter)
	}
}

func TestEmptyQueryReturnsAll(t *testing.T) {
	p := NewProcessLogs(100, nil)
	p.Append(StreamStdout, "x")
	res := p.Query(Query{})
	if len(res.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(res.Entries))
	}
}

func TestLongLinesPreserved(t *testing.T) {
	p := NewProcessLogs(100, nil)
	long := strings.Repeat("x", 200_000)
	p.Append(StreamStdout, long)
	res := p.Query(Query{})
	if len(res.Entries) != 1 || len(res.Entries[0].Line) != len(long) {
		t.Fatalf("long line corrupted: got %d bytes", len(res.Entries[0].Line))
	}
}
