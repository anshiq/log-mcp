//go:build desktop

package gui

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

	"agent-runtime/pkg/client"
)

//go:embed all:dist
var embeddedDist embed.FS

//go:embed icon.png
var trayIcon []byte

var frontendBuildStamp = "unknown"

func Available() bool { return true }

func Run(logger *slog.Logger) error {
	ApplySocketArgs(os.Args[1:])
	return runNative(logger)
}

func OpenDiagnostic(logger *slog.Logger) error {
	ApplySocketArgs(os.Args[1:])
	sock := SocketPath()
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
	_ = logger
	fmt.Printf("agent-runtime GUI: daemon %s api %s reachable at %s.\n",
		v.DaemonVersion, v.APIVersion, sock)
	fmt.Printf("agent-runtime GUI: frontend %s.\n", guiFrontendBuild())
	fmt.Println("Run `agent-runtime` (desktop build) to open the native window.")
	return nil
}

func guiFrontendBuild() string {
	sub, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		return "unknown"
	}
	b, err := fs.ReadFile(sub, ".build.json")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(b))
}

func newProxy(socketPath, agent string) *httputil.ReverseProxy {
	target, _ := url.Parse("http://agentd")
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
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
	notify: function(t,b,tag){ return window.go.gui.App.Notify(t,b,tag||""); },
	openInEditor: function(p,l){ return window.go.gui.App.OpenInEditor(p,l||0); },
	saveDialog: window.go.gui.App.SaveDialog,
	writeFile: window.go.gui.App.WriteFile,
	pickDirectory: window.go.gui.App.PickDirectory,
	revealInFileManager: window.go.gui.App.RevealInFileManager,
	openExternal: window.go.gui.App.OpenExternal,
	setBadge: window.go.gui.App.SetBadge
};}});window.dispatchEvent(new Event("wails:ready"));`

const wailsBindingShim = wailsBindingShimJS + `;window.dispatchEvent(new Event("wails:ready"));`

func runNative(logger *slog.Logger) error {
	sock := SocketPath()
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
	frontendBuildStamp = guiFrontendBuild()
	logger.Info("agent-runtime-gui", "version", Version, "frontend", frontendBuildStamp)
	proxy := newProxy(sock, "gui/"+Version)
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
			if BackgroundMode() {
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
