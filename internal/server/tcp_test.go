package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func freeLoopbackAddr() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr, nil
}

func waitForTCP(t *testing.T, addr string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond); err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("tcp listener never came up at %s", addr)
}

func startTCPServer(t *testing.T) (addr, token string) {
	t.Helper()
	eng, done := testEngine(t)
	t.Cleanup(done)
	tok := "test-token-0123456789abcdef"
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	free, err := freeLoopbackAddr()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(eng, sockPath(t, eng)+".tcp", "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	go func() { errCh <- srv.Serve(ctx) }()
	go func() { errCh <- srv.ServeTCP(free, tok) }()
	waitForTCP(t, free)
	return free, tok
}

func TestWebUI_ServedOverTCP(t *testing.T) {
	addr, _ := startTCPServer(t)

	resp, err := http.Get(fmt.Sprintf("http://%s/", addr))
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "html") {
		t.Fatalf("body does not look like HTML: %s", body)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); csp == "" {
		t.Error("expected a Content-Security-Policy header on the static bundle")
	}
}

func TestWebUI_UnknownPathFallsBackToIndex(t *testing.T) {
	addr, _ := startTCPServer(t)

	resp, err := http.Get(fmt.Sprintf("http://%s/processes/proc_x", addr))
	if err != nil {
		t.Fatalf("GET /processes/proc_x: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (SPA fallback)", resp.StatusCode)
	}
}

func TestWebUI_APIStillRequiresToken(t *testing.T) {
	addr, token := startTCPServer(t)

	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/api/agentruntime.v1.SystemService/GetVersion", addr), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/api/agentruntime.v1.SystemService/GetVersion", addr), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("with token: status = %d, want 200", resp.StatusCode)
	}
}
