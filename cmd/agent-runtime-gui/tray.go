package main

import (
	"context"
	"log/slog"
	"runtime"

	"github.com/getlantern/systray"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// hasTray is false on Linux: getlantern/systray and Wails' webview each
// run their own GTK main loop, and running two in one process crashes
// (confirmed: SIGABRT inside systray.nativeLoop). macOS/Windows use
// native, non-GTK tray APIs and aren't affected.
var hasTray = runtime.GOOS != "linux"

// runTray blocks running the system tray icon; call it in its own
// goroutine from OnStartup. Closing the window hides it (see
// options.App.OnBeforeClose); only the tray's Quit item exits the app.
// The daemon (agentd) is a separate process and is unaffected either way.
func runTray(ctx context.Context, logger *slog.Logger) {
	systray.Run(func() {
		systray.SetTemplateIcon(trayIcon, trayIcon)
		systray.SetTitle("agent-runtime")
		systray.SetTooltip("agent-runtime")

		show := systray.AddMenuItem("Show", "Show the agent-runtime window")
		systray.AddSeparator()
		quit := systray.AddMenuItem("Quit", "Quit agent-runtime-gui (daemon keeps running)")

		go func() {
			for {
				select {
				case <-show.ClickedCh:
					wailsruntime.WindowShow(ctx)
				case <-quit.ClickedCh:
					wailsruntime.Quit(ctx)
					return
				}
			}
		}()
	}, func() {
		logger.Info("tray exited")
	})
}
