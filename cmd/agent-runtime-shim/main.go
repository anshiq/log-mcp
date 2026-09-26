// Command agent-runtime-shim is the per-process supervisor.
// It receives a bundle JSON spec, setsid + PR_SET_CHILD_SUBREAPER,
// creates pipes, execs the child into its cgroup, writes framed
// records to segment files, and serves a control socket.
//
// Usage:
//
//	agent-runtime-shim --bundle <dir>/spec.json [--control <sock>] [--linger 24h]
//
// The shim has no network, no SQLite, no config parsing.
// Its target RSS is ≤ 4 MB.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"agent-runtime/internal/shim"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	if err := run(logger); err != nil {
		logger.Error("agent-runtime-shim", "error", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	var bundlePath, controlSock, lingerStr string
	flag.StringVar(&bundlePath, "bundle", "", "path to spec.json bundle")
	flag.StringVar(&controlSock, "control", "", "control socket path (default <bundle-dir>/shim.sock)")
	flag.StringVar(&lingerStr, "linger", "24h", "linger after exit awaiting Release")
	flag.Parse()

	if bundlePath == "" {
		return fmt.Errorf("usage: agent-runtime-shim --bundle <spec.json>")
	}
	bundle, err := shim.LoadBundle(bundlePath)
	if err != nil {
		return err
	}
	if controlSock == "" {
		controlSock = filepath.Join(filepath.Dir(bundlePath), "shim.sock")
	}
	linger, err := time.ParseDuration(lingerStr)
	if err != nil {
		return fmt.Errorf("bad --linger: %w", err)
	}
	if err := shim.BecomeSubreaper(); err != nil {
		logger.Warn("subreaper not available", "error", err.Error())
	}
	s, err := shim.New(bundle)
	if err != nil {
		return err
	}
	logger.Info("shim starting", "instance", bundle.InstanceID, "argv", bundle.Args)
	return s.Run(controlSock, linger)
}
