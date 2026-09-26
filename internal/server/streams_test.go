package server

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fakeFlusher is a minimal http.ResponseWriter + http.Flusher that lets us
// hammer streamWriter.send concurrently with Close under -race without a
// real network round trip.
type fakeFlusher struct {
	mu      sync.Mutex
	header  http.Header
	flushed int
}

func (f *fakeFlusher) Header() http.Header { return f.header }
func (f *fakeFlusher) Write(b []byte) (int, error) {
	return len(b), nil
}
func (f *fakeFlusher) WriteHeader(int) {}
func (f *fakeFlusher) Flush() {
	f.mu.Lock()
	f.flushed++
	f.mu.Unlock()
}

func TestStreamWriter_ConcurrentSendDuringClose(t *testing.T) {
	ff := &fakeFlusher{header: http.Header{}}
	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	r = r.WithContext(ctx)

	sw, ok := newStream(ff, r, nil)
	if !ok {
		t.Fatal("newStream returned !ok")
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = sw.send(map[string]any{"kind": "line", "i": j})
			}
		}()
	}
	// Close concurrently with the senders above: this reproduces the B1
	// crash (a heartbeat goroutine writing to an already-torn-down
	// response after the request handler returned).
	wg.Add(1)
	go func() {
		defer wg.Done()
		sw.Close()
	}()
	wg.Wait()

	// Sends after Close must be inert, not panic.
	if err := sw.send(map[string]any{"kind": "late"}); err == nil {
		t.Fatal("send after Close should error, not silently succeed")
	}
}

func TestStreamWriter_HeartbeatStopsOnClose(t *testing.T) {
	old := heartbeatInterval
	heartbeatInterval = 5 * time.Millisecond
	defer func() { heartbeatInterval = old }()

	ff := &fakeFlusher{header: http.Header{}}
	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	sw, ok := newStream(ff, r, heartbeatMessage)
	if !ok {
		t.Fatal("newStream returned !ok")
	}
	time.Sleep(30 * time.Millisecond)
	sw.Close()

	ff.mu.Lock()
	flushedAtClose := ff.flushed
	ff.mu.Unlock()

	time.Sleep(30 * time.Millisecond)

	ff.mu.Lock()
	flushedAfter := ff.flushed
	ff.mu.Unlock()

	if flushedAfter != flushedAtClose {
		t.Fatalf("heartbeat kept firing after Close: %d -> %d flushes", flushedAtClose, flushedAfter)
	}
}

func TestDeadlineInterceptor_ExemptsStreamingRoutes(t *testing.T) {
	var gotDeadline bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, gotDeadline = r.Context().Deadline()
	})
	wrapped := DeadlineInterceptor(next)

	for path := range streamingRoutes {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		wrapped.ServeHTTP(httptest.NewRecorder(), req)
		if gotDeadline {
			t.Errorf("streaming route %s got a request deadline; it must stay open indefinitely", path)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/agentruntime.v1.SystemService/GetVersion", nil)
	wrapped.ServeHTTP(httptest.NewRecorder(), req)
	if !gotDeadline {
		t.Error("unary route did not get a request deadline")
	}
}

// TestServer_StreamClientDisconnect_DoesNotCrashDaemon is the end-to-end
// regression test for B1: a real client opening a stream and then closing
// the connection abruptly (not a clean HTTP close) used to panic inside a
// bare goroutine — outside any per-request recover middleware — and take
// the whole daemon process down. This dials the real unix socket, opens
// WatchProcesses, and slams the connection shut mid-stream; the server
// must still answer a plain request afterwards.
func TestServer_StreamClientDisconnect_DoesNotCrashDaemon(t *testing.T) {
	// Deliberately left at the production heartbeat interval: overriding it
	// here would race the background heartbeat goroutines of streams whose
	// client-disconnect the server hasn't yet noticed (a client Close() is
	// detected asynchronously by net/http, not synchronously by this test).
	eng, done := testEngine(t)
	defer done()
	sock := sockPath(t, eng)

	for i := 0; i < 20; i++ {
		conn, err := net.DialTimeout("unix", sock, time.Second)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		req, _ := http.NewRequest(http.MethodPost, "http://agentd/agentruntime.v1.ProcessService/WatchProcesses", nil)
		if err := req.Write(conn); err != nil {
			t.Fatalf("write request: %v", err)
		}
		// Read a byte or two of the response (enough to know the stream
		// opened) then slam the raw connection shut instead of doing a
		// graceful HTTP close, to reproduce the abrupt-disconnect path.
		br := bufio.NewReader(conn)
		buf := make([]byte, 64)
		_, _ = br.Read(buf)
		time.Sleep(3 * time.Millisecond)
		_ = conn.Close()
	}

	// The daemon must still be alive and serving.
	conn, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		t.Fatalf("daemon did not survive concurrent stream disconnects: %v", err)
	}
	defer conn.Close()
	req, _ := http.NewRequest(http.MethodPost, "http://agentd/agentruntime.v1.SystemService/GetVersion", nil)
	if err := req.Write(conn); err != nil {
		t.Fatalf("write request after disconnect storm: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		t.Fatalf("read response after disconnect storm: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
