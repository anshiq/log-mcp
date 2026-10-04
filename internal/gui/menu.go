//go:build desktop

package gui

import (
	"github.com/wailsapp/wails/v2/pkg/menu"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func buildMenu() *menu.Menu {
	m := menu.NewMenu()
	appMenu := m.AddSubmenu("App")
	appMenu.AddText("About", nil, func(cd *menu.CallbackData) {})
	appMenu.AddText("Settings", nil, func(cd *menu.CallbackData) {
		if appCtx != nil {
			wailsruntime.WindowExecJS(appCtx, `location.hash="#/settings"`)
		}
	})
	appMenu.AddSeparator()
	appMenu.AddText("Quit", nil, func(cd *menu.CallbackData) {
		if appCtx != nil {
			wailsruntime.Quit(appCtx)
		}
	})
	viewMenu := m.AddSubmenu("View")
	viewMenu.AddText("Reload", nil, func(cd *menu.CallbackData) {
		if appCtx != nil {
			wailsruntime.WindowReload(appCtx)
		}
	})
	viewMenu.AddText("Zoom In", nil, func(cd *menu.CallbackData) {})
	viewMenu.AddText("Zoom Out", nil, func(cd *menu.CallbackData) {})
	goMenu := m.AddSubmenu("Go")
	for _, it := range []struct{ label, route string }{
		{"Overview", "#/"}, {"Processes", "#/processes"}, {"Logs", "#/logs"},
		{"Apps", "#/apps"}, {"Config", "#/config"}, {"Events", "#/events"},
		{"Projects", "#/projects"}, {"Sessions", "#/sessions"},
		{"Integrations", "#/integrations"}, {"Audit", "#/audit"}, {"Settings", "#/settings"},
	} {
		route := it.route
		goMenu.AddText(it.label, nil, func(cd *menu.CallbackData) {
			if appCtx != nil {
				wailsruntime.WindowExecJS(appCtx, `location.hash="`+route+`"`)
			}
		})
	}
	helpMenu := m.AddSubmenu("Help")
	helpMenu.AddText("Docs", nil, func(cd *menu.CallbackData) {
		if appCtx != nil {
			wailsruntime.BrowserOpenURL(appCtx, "https://github.com/anshiq/log-mcp")
		}
	})
	helpMenu.AddText("Report issue", nil, func(cd *menu.CallbackData) {
		if appCtx != nil {
			wailsruntime.BrowserOpenURL(appCtx, "https://github.com/anshiq/log-mcp/issues")
		}
	})
	return m
}
