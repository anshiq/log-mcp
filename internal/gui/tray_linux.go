//go:build desktop && linux

package gui

import (
	"context"
	"log/slog"
)

var hasTray = false

func runTray(_ context.Context, _ *slog.Logger) {}
