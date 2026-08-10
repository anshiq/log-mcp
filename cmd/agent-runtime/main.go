// Command agent-runtime is a local, asynchronous process supervisor for AI
// coding agents. It exposes a stdio MCP server so agents such as Claude Code,
// Codex and Gemini CLI can start, monitor, query and stop development processes
// (Next.js, Spring Boot, Django, ...) without flooding the conversation with
// log output.
package main

import (
	"log/slog"
	"os"

	"agent-runtime/internal/cli"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	if err := cli.Run(os.Args[1:], logger); err != nil {
		logger.Error("agent-runtime", "error", err.Error())
		os.Exit(1)
	}
}
