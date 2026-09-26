// Command agentd is the always-on daemon that hosts every
// project runtime, the log pipeline, and the event bus.
// It is started on demand by the first agent or by systemd
// socket activation, and runs until explicitly stopped.
//
// Usage:
//
//	agentd [--foreground] [--socket PATH] [--data DIR]
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"agent-runtime/internal/core"
	"agent-runtime/internal/daemon"
	"agent-runtime/internal/platform/paths"
	"agent-runtime/internal/server"
	"agent-runtime/internal/store"
)

var version = "v3.0.0-dev"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	if err := run(logger); err != nil {
		logger.Error("agentd", "error", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	var foreground bool
	var socketOverride, dataOverride string
	flag.BoolVar(&foreground, "foreground", false, "run in foreground (no double-fork)")
	flag.StringVar(&socketOverride, "socket", "", "override daemon socket path")
	flag.StringVar(&dataOverride, "data", "", "override data dir")
	flag.Parse()
	_ = foreground // foreground is the only mode v3 supports; kept for CLI compat

	p := paths.User()
	socketPath := p.SocketPath()
	if socketOverride != "" {
		socketPath = socketOverride
	}
	dataDir := p.Data
	if dataOverride != "" {
		dataDir = dataOverride
	}
	if err := p.EnsureDirs(); err != nil {
		return fmt.Errorf("ensure dirs: %w", err)
	}

	d := daemon.New(socketPath, dataDir)
	lock, err := d.AcquireLock()
	if err != nil {
		return err
	}
	defer lock.Close()

	if err := d.WritePid(); err != nil {
		return fmt.Errorf("write pid: %w", err)
	}
	defer os.Remove(socketPath + ".pid")

	dbPath := dataDir + "/state.db"
	if dataOverride != "" {
		dbPath = dataDir + "/state.db"
	} else {
		dbPath = p.StateDBPath()
	}
	db, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open state.db: %w", err)
	}
	defer db.Close()

	engine := core.New(db)
	defer engine.Close()

	// Reconnect shims from the previous generation before serving.
	if lives, err := db.LiveInstances(); err == nil && len(lives) > 0 {
		var in []daemon.LiveInstance
		for _, l := range lives {
			in = append(in, daemon.LiveInstance{InstanceID: l.ID, ShimDir: l.ShimDir})
		}
		rep := d.ReconnectShims(p.Runtime, in)
		logger.Info("reconnected shims", "reconnected", len(rep.Reconnected), "orphaned", len(rep.Orphaned))
	}

	srv, err := server.New(engine, socketPath, version)
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		if err := srv.Serve(ctx); err != nil {
			logger.Error("api server", "error", err.Error())
		}
	}()

	daemon.NotifyReady()
	logger.Info("agentd ready", "socket", socketPath, "state", dbPath, "version", version)

	// SIGQUIT = stop --all (stop processes first); TERM/INT = keep-processes
	// drain (shims detached, next daemon re-attaches); HUP = reload.
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGHUP)
	for sig := range ch {
		switch sig {
		case syscall.SIGHUP:
			logger.Info("reload (SIGHUP): config watcher picks up changes; nothing to do")
		case syscall.SIGQUIT:
			logger.Info("agentd draining with --all: stopping processes first (Phase 3 wires manager)")
			return nil
		default:
			logger.Info("agentd draining: detaching shims, processes keep running", "signal", sig.String())
			return nil
		}
	}
	return nil
}
