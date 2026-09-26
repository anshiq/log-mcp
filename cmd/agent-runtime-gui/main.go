// Command agent-runtime-gui is the desktop application shell.
//
// It ensures the daemon, serves the embedded frontend (ui/dist, go:embed)
// and reverse-proxies /api to agentd.sock with streaming flush, inside a
// native Wails v2 window with a system tray icon. Closing the window hides
// it; the tray's Quit item exits agent-runtime-gui but never the daemon.
// `gui open` prints daemon reachability without opening a window.
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
	"strings"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"agent-runtime/internal/platform/paths"
	"agent-runtime/pkg/client"
)

//go:embed all:dist
var embeddedDist embed.FS

//go:embed icon.png
var trayIcon []byte

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
	return runNative(logger)
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
	fmt.Println("Run `agent-runtime-gui` (no args) to open the native window.")
	return nil
}

// newProxy returns a reverse proxy from /api to the daemon UDS.
// Streaming responses flush immediately (FlushInterval -1).
func newProxy(socketPath, agent string) *httputil.ReverseProxy {
	target, _ := url.Parse("http://agentd")
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			// The webview calls /api/<Service>/<Method>; the daemon
			// serves /<Service>/<Method>. Strip the prefix here so one
			// path scheme works for the native shell, web UI and curl.
			r.Out.URL.Path = strings.TrimPrefix(r.Out.URL.Path, "/api")
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

// apiMiddleware routes /api/* to proxy and everything else to next
// (the embedded frontend), so Wails' AssetServer can serve both from
// one origin with no CORS.
func apiMiddleware(proxy *httputil.ReverseProxy) assetserver.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				proxy.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// wailsBindingShim bridges Wails' auto-injected window.go.main.App.*
// bindings to the window.__wailsBinding contract ui/src/lib/platform.ts
// already expects, with no changes needed in ui/.
const wailsBindingShim = `window.__wailsBinding = {
	notify: window.go.main.App.Notify,
	openInEditor: window.go.main.App.OpenInEditor,
	saveDialog: window.go.main.App.SaveDialog,
	writeFile: window.go.main.App.WriteFile
};`

func runNative(logger *slog.Logger) error {
	p := paths.User()
	if _, err := client.EnsureDaemon(p.SocketPath()); err != nil {
		return fmt.Errorf("gui: %w", err)
	}
	sub, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		return fmt.Errorf("gui: embedded frontend missing (build ui/ first): %w", err)
	}
	proxy := newProxy(p.SocketPath(), "gui/3.0.0")
	app := &App{}

	return wails.Run(&options.App{
		Title:  "agent-runtime",
		Width:  1100,
		Height: 750,
		AssetServer: &assetserver.Options{
			Assets:     sub,
			Middleware: apiMiddleware(proxy),
		},
		OnStartup: func(ctx context.Context) {
			app.startup(ctx)
			if hasTray {
				go runTray(ctx, logger)
			}
		},
		OnDomReady: func(ctx context.Context) {
			wailsruntime.WindowExecJS(ctx, wailsBindingShim)
		},
		OnBeforeClose: func(ctx context.Context) (prevent bool) {
			if !hasTray {
				// No tray to reopen the window from (see hasTray):
				// closing the window quits like a normal app.
				return false
			}
			// Closing the window leaves the tray running; only the
			// tray's Quit item calls runtime.Quit. The daemon is a
			// separate process and is unaffected either way.
			wailsruntime.WindowHide(ctx)
			return true
		},
		Bind: []interface{}{app},
		Linux: &linux.Options{
			Icon: trayIcon,
		},
	})
}
