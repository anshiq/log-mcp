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
	wg.Add(1)
	go func() {
		defer wg.Done()
		sw.Close()
	}()
	wg.Wait()

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

	time.Sleep(50 * time.Millisecond)

	ff.mu.Lock()
	flushedAfter := ff.flushed
	ff.mu.Unlock()

	if flushedAfter > flushedAtClose+1 {
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

func TestServer_StreamClientDisconnect_DoesNotCrashDaemon(t *testing.T) {
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
		br := bufio.NewReader(conn)
		buf := make([]byte, 64)
		_, _ = br.Read(buf)
		time.Sleep(3 * time.Millisecond)
		_ = conn.Close()
	}

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
