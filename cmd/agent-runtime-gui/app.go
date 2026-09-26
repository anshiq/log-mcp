package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

var terminalEditors = map[string]bool{
	"vim": true, "vi": true, "nvim": true, "nano": true,
	"hx": true, "helix": true, "emacs": true, "micro": true,
	"joe": true, "ne": true, "pico": true,
}

var terminalEmulatorFlags = []struct {
	bin  string
	args []string
}{
	{"x-terminal-emulator", []string{"-e"}},
	{"gnome-terminal", []string{"--"}},
	{"konsole", []string{"-e"}},
	{"alacritty", []string{"-e"}},
	{"kitty", nil},
	{"xterm", []string{"-e"}},
}

// OpenInEditor opens path in $VISUAL/$EDITOR, falling back to the
// platform's default opener.
func (a *App) OpenInEditor(path string) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return openWithSystemDefault(path)
	}
	name := filepath.Base(editor)
	if terminalEditors[name] {
		return launchInTerminal(editor, path)
	}
	return exec.Command(editor, path).Start()
}

func launchInTerminal(editor, path string) error {
	if term := os.Getenv("TERMINAL"); term != "" {
		if _, err := exec.LookPath(term); err == nil {
			return exec.Command(term, "-e", editor, path).Start()
		}
	}
	for _, cand := range terminalEmulatorFlags {
		if _, err := exec.LookPath(cand.bin); err != nil {
			continue
		}
		args := append(append([]string{}, cand.args...), editor, path)
		return exec.Command(cand.bin, args...).Start()
	}
	return errors.New("no terminal emulator found to run " + editor)
}

func openWithSystemDefault(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", "", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
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
