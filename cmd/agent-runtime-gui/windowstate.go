package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"agent-runtime/internal/platform/paths"
)

type windowState struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Maximised bool `json:"maximised"`
}

const (
	defaultWindowWidth  = 1100
	defaultWindowHeight = 750
	minWindowWidth      = 900
	minWindowHeight     = 600
)

func windowStatePath() string {
	return filepath.Join(paths.User().Config, "gui.json")
}

func loadWindowState() windowState {
	st := windowState{Width: defaultWindowWidth, Height: defaultWindowHeight}
	data, err := os.ReadFile(windowStatePath())
	if err != nil {
		return st
	}
	_ = json.Unmarshal(data, &st)
	if st.Width < minWindowWidth {
		st.Width = defaultWindowWidth
	}
	if st.Height < minWindowHeight {
		st.Height = defaultWindowHeight
	}
	return st
}

func saveWindowState(ctx context.Context) {
	st := windowState{Maximised: wailsruntime.WindowIsMaximised(ctx)}
	st.Width, st.Height = wailsruntime.WindowGetSize(ctx)
	st.X, st.Y = wailsruntime.WindowGetPosition(ctx)
	data, err := json.Marshal(st)
	if err != nil {
		return
	}
	path := windowStatePath()
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, data, 0o600)
}

func applyWindowState(ctx context.Context, st windowState) {
	if st.Maximised {
		wailsruntime.WindowMaximise(ctx)
		return
	}
	if st.X != 0 || st.Y != 0 {
		wailsruntime.WindowSetPosition(ctx, st.X, st.Y)
	}
}
