// Package httpserve exposes the MCP server over the streamable-HTTP transport
// (Phase 5) with a security posture suitable for a shared/remote runtime: a
// required bearer token (no unauthenticated mode, even on localhost) and a
// per-token rate limit (token bucket) so the network edge stays bounded.
package httpserve

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/time/rate"
)

// Defaults for the network-edge rate limit (per token).
const (
	defaultRate  = 20.0 // tokens per second
	defaultBurst = 50   // burst capacity
)

// Handler wraps an *mcp.Server in the streamable-HTTP handler behind auth and
// rate-limit middleware. token is the required bearer token (already
// env-expanded); if empty, every request is refused (503) — there is no
// unauthenticated HTTP mode.
func Handler(server *mcp.Server, token string, logger *slog.Logger) http.Handler {
	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		Logger: logger,
		// Sessionless, modern-protocol mode: keeps per-client state minimal at
		// the transport layer while the shared registry (daemon semantics)
		// stays process-scoped, not session-scoped.
		Stateless: true,
	})

	var h http.Handler = streamable
	h = rateLimit(h, token, defaultRate, defaultBurst)
	h = auth(h, token)
	return h
}

// auth enforces the required bearer token. Requests without the exact
// "Bearer <token>" header get 401. A missing configured token refuses all
// requests with 503 so a misconfiguration is loud, never a silent open server.
func auth(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			http.Error(w, "agent-runtime http transport requires runtime.http.token", http.StatusServiceUnavailable)
			return
		}
		if !bearerMatches(r.Header.Get("Authorization"), token) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerMatches(header, token string) bool {
	parts := strings.SplitN(header, " ", 2)
	return len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && parts[1] == token
}

// rateLimiter applies a token bucket per key (here: the token, so all clients
// sharing the token share the budget, bounding total tool-call rate).
type rateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	r        rate.Limit
	b        int
}

func newRateLimiter(r rate.Limit, b int) *rateLimiter {
	return &rateLimiter{limiters: make(map[string]*rate.Limiter), r: r, b: b}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	l, ok := rl.limiters[key]
	if !ok {
		l = rate.NewLimiter(rl.r, rl.b)
		rl.limiters[key] = l
	}
	rl.mu.Unlock()
	return l.Allow()
}

func rateLimit(next http.Handler, token string, r float64, burst int) http.Handler {
	rl := newRateLimiter(rate.Limit(r), burst)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !rl.allow(token) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, req)
	})
}
