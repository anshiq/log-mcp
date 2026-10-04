//go:build desktop && !linux

package gui

import (
	"context"
	"log/slog"

	"github.com/getlantern/systray"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var hasTray = true

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
