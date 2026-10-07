package httpserve

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-runtime/internal/config"
	rtmcp "agent-runtime/internal/mcp"
	"agent-runtime/internal/runtime"
)

func testHandler(t *testing.T, token string) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// nil *mcp.Server: the streamable handler serves 400 for valid-token
	// requests, which is enough to exercise auth + rate-limit middleware.
	return Handler(nil, token, logger)
}

func TestAuthRequired(t *testing.T) {
	ts := httptest.NewServer(testHandler(t, "s3cr3t"))
	defer ts.Close()

	// No token -> 401.
	if code := status(t, ts.URL, ""); code != http.StatusUnauthorized {
		t.Fatalf("no token status = %d, want 401", code)
	}
	// Wrong token -> 401.
	if code := status(t, ts.URL, "Bearer wrong"); code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want 401", code)
	}
	// Missing configured token refuses everything (503), not silent 200.
	ts2 := httptest.NewServer(testHandler(t, ""))
	defer ts2.Close()
	if code := status(t, ts2.URL, ""); code != http.StatusServiceUnavailable {
		t.Fatalf("missing token config status = %d, want 503", code)
	}
	// Correct token -> not 401/503 (the streamable handler may 400 on bad body).
	code := status(t, ts.URL, "Bearer s3cr3t")
	if code == http.StatusUnauthorized || code == http.StatusServiceUnavailable {
		t.Fatalf("valid token status = %d, want allowed", code)
	}
}

func TestRateLimitExceeded(t *testing.T) {
	ts := httptest.NewServer(testHandler(t, "tok"))
	defer ts.Close()
	seen429 := false
	for i := 0; i < 60; i++ {
		if status(t, ts.URL, "Bearer tok") == http.StatusTooManyRequests {
			seen429 = true
			break
		}
	}
	if !seen429 {
		t.Fatal("expected at least one 429 after exceeding the token bucket burst")
	}
}

func status(t *testing.T, url, auth string) int {
	t.Helper()
	req, err := http.NewRequest("POST", url, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// TestInitializeHandshake drives a real MCP initialize over HTTP with a valid
// bearer token and a real local runtime facade, proving the full serve --http
// wiring (MCP server over facade, wrapped in auth + streamable handler).
func TestInitializeHandshake(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent-runtime.yaml"), []byte("runtime:\n  shell_env: none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadFile(filepath.Join(dir, "agent-runtime.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	rt := runtime.New(loaded, logger)
	t.Cleanup(func() { rt.Shutdown() })

	server := rtmcp.NewServer(rt, logger)
	ts := httptest.NewServer(Handler(server, "tok", logger))
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
	req, err := http.NewRequest("POST", ts.URL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize status = %d, want 200; body=%s", resp.StatusCode, out)
	}
	if !strings.Contains(string(out), "agent-runtime") {
		t.Fatalf("initialize response missing serverInfo: %s", out)
	}
}
