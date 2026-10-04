//go:build desktop

package gui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/gen2brain/beeep"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"agent-runtime/pkg/client"
)

type App struct {
	ctx context.Context
}

var appCtx context.Context

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	appCtx = ctx
}

func (a *App) Notify(title, body string, tag string) {
	_ = beeep.Notify(title, body, "")
	_ = tag
}

func (a *App) Version() string {
	return Version
}

func (a *App) FrontendBuild() string {
	return frontendBuildStamp
}

func (a *App) SocketPath() string {
	return SocketPath()
}

func (a *App) EnsureDaemon() error {
	_, err := client.EnsureDaemon(SocketPath())
	return err
}

func taggedTitle(count int) string {
	return fmt.Sprintf("(%d) agent-runtime", count)
}

func (a *App) OpenExternal(url string) error {
	return openWithSystemDefault(url)
}

func (a *App) PickDirectory() (string, error) {
	if a.ctx == nil {
		return "", errors.New("app: not started")
	}
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{})
}

func (a *App) RevealInFileManager(path string) error {
	return openWithSystemDefault(filepath.Dir(path))
}

func (a *App) SetBadge(count int) {
	if a.ctx == nil {
		return
	}
	if count > 0 {
		wailsruntime.WindowSetTitle(a.ctx, taggedTitle(count))
	} else {
		wailsruntime.WindowSetTitle(a.ctx, "agent-runtime")
	}
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

func (a *App) OpenInEditor(path string, line int) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return openWithSystemDefault(path)
	}
	name := filepath.Base(editor)
	base := name
	if idx := len(base); idx > 0 {
		_ = idx
	}
	if terminalEditors[name] {
		if line > 0 {
			arg := path + ":" + itoa(line)
			return launchInTerminal(editor, arg)
		}
		return launchInTerminal(editor, path)
	}
	switch name {
	case "code", "codium", "code-insiders":
		if line > 0 {
			return exec.Command(editor, "-g", path+":"+itoa(line)).Start()
		}
		return exec.Command(editor, path).Start()
	case "subl", "zed", "code-oss":
		if line > 0 {
			return exec.Command(editor, path+":"+itoa(line)).Start()
		}
		return exec.Command(editor, path).Start()
	case "idea", "webstorm", "goland", "pycharm":
		if line > 0 {
			return exec.Command(editor, "--line", itoa(line), path).Start()
		}
		return exec.Command(editor, path).Start()
	case "gedit", "kate":
		if line > 0 {
			return exec.Command(editor, "+"+itoa(line), path).Start()
		}
		return exec.Command(editor, path).Start()
	}
	_ = base
	return exec.Command(editor, path).Start()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var b [32]byte
	pos := len(b)
	for n > 0 {
		pos--
		b[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
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

func (a *App) SaveDialog(suggested string) (string, error) {
	if a.ctx == nil {
		return "", errors.New("app: not started")
	}
	return wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		DefaultFilename: suggested,
	})
}

func (a *App) WriteFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
