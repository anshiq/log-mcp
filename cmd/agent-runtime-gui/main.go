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

var version = "v0.4.0"

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
	for i, a := range os.Args {
		if a == "--socket" && i+1 < len(os.Args) {
			_ = os.Setenv("AGENTD_SOCKET", os.Args[i+1])
		}
		if strings.HasPrefix(a, "--socket=") {
			_ = os.Setenv("AGENTD_SOCKET", strings.TrimPrefix(a, "--socket="))
		}
	}
	if len(os.Args) > 1 && os.Args[1] == "open" {
		return openGUI(logger)
	}
	return runNative(logger)
}

func socketPath() string {
	if s := os.Getenv("AGENTD_SOCKET"); s != "" {
		return s
	}
	return paths.User().SocketPath()
}

func backgroundMode() bool {
	for _, a := range os.Args {
		if a == "--background" {
			return true
		}
	}
	return false
}

func openGUI(logger *slog.Logger) error {
	sock := socketPath()
	c, err := client.EnsureDaemon(sock)
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
		v.DaemonVersion, v.APIVersion, sock)
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

func apiMiddleware(proxy *httputil.ReverseProxy) assetserver.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/__wails_shim.js" {
				w.Header().Set("Content-Type", "application/javascript")
				_, _ = w.Write([]byte(wailsBindingShimJS))
				return
			}
			if strings.HasPrefix(r.URL.Path, "/api/") {
				proxy.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

const wailsBindingShimJS = `Object.defineProperty(window,"__wailsBinding",{configurable:true,get:function(){return {
	notify: function(t,b,tag){ return window.go.main.App.Notify(t,b,tag||""); },
	openInEditor: function(p,l){ return window.go.main.App.OpenInEditor(p,l||0); },
	saveDialog: window.go.main.App.SaveDialog,
	writeFile: window.go.main.App.WriteFile,
	pickDirectory: window.go.main.App.PickDirectory,
	revealInFileManager: window.go.main.App.RevealInFileManager,
	openExternal: window.go.main.App.OpenExternal,
	setBadge: window.go.main.App.SetBadge
};}});window.dispatchEvent(new Event("wails:ready"));`

const wailsBindingShim = wailsBindingShimJS + `;window.dispatchEvent(new Event("wails:ready"));`

func runNative(logger *slog.Logger) error {
	sock := socketPath()
	if _, err := client.EnsureDaemon(sock); err != nil {
		return fmt.Errorf("gui: %w", err)
	}
	sub, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		return fmt.Errorf("gui: embedded frontend missing (build ui/ first): %w", err)
	}
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return fmt.Errorf("gui: embedded frontend missing index.html (run make build-gui): %w", err)
	}
	if !strings.Contains(string(index), `id="app"`) {
		return fmt.Errorf("gui: embedded frontend is stale placeholder (run make build-gui to rebuild ui/)")
	}
	entries, err := fs.ReadDir(sub, "assets")
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("gui: embedded frontend has no assets (run make build-gui to rebuild ui/)")
	}
	proxy := newProxy(sock, "gui/"+version)
	app := &App{}
	geometry := loadWindowState()
	if geometry.Width < 900 {
		geometry.Width = 1100
	}
	if geometry.Height < 600 {
		geometry.Height = 750
	}

	return wails.Run(&options.App{
		Title:     "agent-runtime",
		Width:     geometry.Width,
		Height:    geometry.Height,
		MinWidth:  900,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets:     sub,
			Middleware: apiMiddleware(proxy),
		},
		Menu: buildMenu(),
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "agent-runtime-gui",
			OnSecondInstanceLaunch: func(_ options.SecondInstanceData) {
				wailsruntime.WindowShow(app.ctx)
				wailsruntime.WindowUnminimise(app.ctx)
			},
		},
		OnStartup: func(ctx context.Context) {
			app.startup(ctx)
			applyWindowState(ctx, geometry)
			if hasTray {
				go runTray(ctx, logger)
			}
		},
		OnDomReady: func(ctx context.Context) {
			wailsruntime.WindowExecJS(ctx, wailsBindingShim)
		},
		OnBeforeClose: func(ctx context.Context) (prevent bool) {
			saveWindowState(ctx)
			if backgroundMode() {
				wailsruntime.WindowHide(ctx)
				return true
			}
			if !hasTray {
				return false
			}
			wailsruntime.WindowHide(ctx)
			return true
		},
		Bind: []interface{}{app},
		Linux: &linux.Options{
			Icon: trayIcon,
		},
	})
}
