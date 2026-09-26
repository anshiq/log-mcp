// Command agent-runtime-gui is the desktop application shell.
// Framework touchpoints are isolated here so a later move (Wails v2/v3,
// Tauri) only affects this package.
//
// Today it: ensures the daemon, serves the embedded frontend (ui/dist,
// go:embed) and reverse-proxies /api to agentd.sock with streaming
// flush. The Wails window/tray/notifications bind onto this same proxy
// in Phase 7 final assembly; `gui open` prints the local URL.
package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"agent-runtime/internal/platform/paths"
	"agent-runtime/pkg/client"
)

//go:embed all:dist
var embeddedDist embed.FS

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	if err := run(logger); err != nil {
		logger.Error("agent-runtime-gui", "error", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	if len(os.Args) > 1 && os.Args[1] == "open" {
		return openGUI(logger)
	}
	// Default: run the local shell (frontend + /api proxy). The Wails
	// window loads this same origin; closing the window leaves the tray
	// (and never stops the daemon).
	return serveShell(logger, "127.0.0.1:0")
}

func openGUI(logger *slog.Logger) error {
	p := paths.User()
	c, err := client.EnsureDaemon(p.SocketPath())
	if err != nil {
		return fmt.Errorf("gui: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	v, err := c.GetVersion(ctx)
	if err != nil {
		return fmt.Errorf("gui: daemon handshake: %w", err)
	}
	fmt.Printf("agent-runtime GUI: daemon %s api %s reachable at %s.\n",
		v.DaemonVersion, v.APIVersion, p.SocketPath())
	fmt.Println("Run `agent-runtime-gui` (no args) for the local shell, then open the printed URL.")
	return nil
}

// newProxy returns a reverse proxy from /api to the daemon UDS.
// Streaming responses flush immediately (FlushInterval -1).
func newProxy(socketPath, agent string) *httputil.ReverseProxy {
	target, _ := url.Parse("http://agentd")
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Header.Set("X-Agent-Runtime-Client", agent)
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
		FlushInterval: -1,
	}
	return proxy
}

func serveShell(logger *slog.Logger, addr string) error {
	p := paths.User()
	if _, err := client.EnsureDaemon(p.SocketPath()); err != nil {
		return fmt.Errorf("gui: %w", err)
	}
	mux := http.NewServeMux()
	proxy := newProxy(p.SocketPath(), "gui/3.0.0")
	mux.Handle("/api/", proxy)
	sub, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		return fmt.Errorf("gui: embedded frontend missing (build ui/ first): %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Printf("agent-runtime GUI shell at http://%s (proxying /api to %s)\n", ln.Addr(), p.SocketPath())
	return srv.Serve(ln)
}
