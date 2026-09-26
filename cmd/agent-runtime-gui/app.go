package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"

	"github.com/gen2brain/beeep"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is bound to the webview and implements the window.__wailsBinding
// contract declared in ui/src/lib/platform.ts: notify, openInEditor,
// saveDialog, writeFile.
type App struct {
	ctx context.Context
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// Notify shows a native desktop notification.
func (a *App) Notify(title, body string) {
	_ = beeep.Notify(title, body, "")
}

// OpenInEditor opens path in $VISUAL/$EDITOR, falling back to the
// platform's default opener.
func (a *App) OpenInEditor(path string) {
	if editor := os.Getenv("VISUAL"); editor != "" {
		_ = exec.Command(editor, path).Start()
		return
	}
	if editor := os.Getenv("EDITOR"); editor != "" {
		_ = exec.Command(editor, path).Start()
		return
	}
	_ = exec.Command(platformOpener(), path).Start()
}

func platformOpener() string {
	switch runtime.GOOS {
	case "darwin":
		return "open"
	case "windows":
		return "start"
	default:
		return "xdg-open"
	}
}

// SaveDialog shows a native "save as" dialog and returns the chosen path,
// or "" if the user cancelled.
func (a *App) SaveDialog(suggested string) (string, error) {
	if a.ctx == nil {
		return "", errors.New("app: not started")
	}
	return wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		DefaultFilename: suggested,
	})
}

// WriteFile writes content to path, used after SaveDialog resolves.
func (a *App) WriteFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
