// TCP listener for the web UI and remote tooling (§5.5, Phase 9 remote
// daemon). Disabled by default; enabled with agentd --tcp 127.0.0.1:7350.
// Auth: bearer token (from the token file, 0600, generated when absent),
// strict Host (127.0.0.1:port/localhost:port) and Origin checks
// (DNS-rebinding defence), no permissive CORS, token never in cookies.
// Remote hosts reach it over SSH-forwarded sockets/ports.
package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// EnsureToken reads the bearer token, generating + storing one (0600)
// when absent.
func EnsureToken(tokenPath string) (string, error) {
	if data, err := os.ReadFile(tokenPath); err == nil {
		if tok := strings.TrimSpace(string(data)); tok != "" {
			return tok, nil
		}
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b[:])
	if err := os.MkdirAll(dirOf(tokenPath), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(tokenPath, []byte(tok+"\n"), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

// tcpGuard enforces bearer auth, Host/Origin checks and rate limiting.
func tcpGuard(next http.Handler, token string) http.Handler {
	limiter := rate.NewLimiter(rate.Every(time.Second/20), 40) // 20 rps burst 40
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow() {
			http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
			return
		}
		// Host check: must be loopback (DNS-rebinding defence).
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			http.Error(w, `{"error":"forbidden host"}`, http.StatusForbidden)
			return
		}
		// Origin check: browsers only send whitelisted origins.
		if origin := r.Header.Get("Origin"); origin != "" {
			ok := false
			for _, prefix := range []string{"http://127.0.0.1:", "http://localhost:", "http://[::1]:"} {
				if strings.HasPrefix(origin, prefix) {
					ok = true
					break
				}
			}
			if !ok {
				http.Error(w, `{"error":"forbidden origin"}`, http.StatusForbidden)
				return
			}
		}
		// Bearer token (login screen pastes it; GUI `web open` prints a
		// one-time URL with #token=... — fragment never hits the wire).
		if got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); got != token {
			w.Header().Set("WWW-Authenticate", `Bearer realm="agent-runtime"`)
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// stripAPIPrefix lets the TCP listener serve both /api/<svc>/<m> (web UI
// fetch paths) and bare /<svc>/<m> paths.
func stripAPIPrefix(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			r2 := r.Clone(r.Context())
			r2.URL.Path = strings.TrimPrefix(r.URL.Path, "/api")
			next.ServeHTTP(w, r2)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Blocks until ctx done or error; call sites run it alongside Serve.
func (s *Server) ServeTCP(addr, token string) error {
	inner := tcpGuard(stripAPIPrefix(s.handler), token)
	srv := &http.Server{
		Addr: addr, Handler: inner,
		ReadHeaderTimeout: 5 * time.Second,
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("server: tcp listen %s: %w", addr, err)
	}
	return srv.Serve(ln)
}
