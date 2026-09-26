// Command agent-runtime-gui is the desktop application built
// with Wails (Go) + TypeScript/Svelte frontend.
// The Go side is only a window, tray icon, notifications,
// and a proxy from the webview to the daemon socket.
//
// The Wails dependency lands in Phase 7; until then this binary
// ensures the daemon is reachable and prints how to use the web
// build of ui/ against the TCP listener (Phase 9).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"agent-runtime/internal/platform/paths"
	"agent-runtime/pkg/client"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	if err := run(logger); err != nil {
		logger.Error("agent-runtime-gui", "error", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	p := paths.User()
	c, err := client.EnsureDaemon(p.SocketPath())
	if err != nil {
		return fmt.Errorf("gui: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	v, err := c.GetVersion(ctx)
	if err != nil {
		return fmt.Errorf("gui: daemon handshake: %w", err)
	}
	fmt.Printf("agent-runtime GUI (Phase 7): daemon %s api %s reachable at %s.\n",
		v.DaemonVersion, v.APIVersion, p.SocketPath())
	fmt.Println("The Wails shell is not vendored yet; use `agent-runtime project resolve` + API until Phase 7 lands.")
	return nil
}
