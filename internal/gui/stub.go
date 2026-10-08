//go:build !desktop

package gui

import (
	"errors"
	"log/slog"
)

func Available() bool { return false }

func Run(_ *slog.Logger) error {
	return errors.New("GUI not in this build: run `agent-runtime web` for the web interface, or install the desktop build (`make build-gui`)")
}

func OpenDiagnostic(_ *slog.Logger) error {
	return errors.New("GUI not in this build: run `agent-runtime web` for the web interface, or install the desktop build (`make build-gui`)")
}
