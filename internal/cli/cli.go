// Package cli implements the agent-runtime command-line interface. The primary
// mode is the stdio MCP server ("serve"); run/integrate/version are debugging
// and setup helpers.
package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"agent-runtime/internal/config"
	"agent-runtime/internal/integrate"
	rtmcp "agent-runtime/internal/mcp"
	"agent-runtime/internal/runtime"
)

// Run dispatches the top-level subcommand. Invoked from main with os.Args[1:].
func Run(args []string, logger *slog.Logger) error {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "serve":
		loaded, err := config.LoadDefault()
		if err != nil {
			return err
		}
		return Serve(loaded, logger)
	case "run":
		loaded, err := config.LoadDefault()
		if err != nil {
			return err
		}
		return RunCommand(args[1:], loaded, logger)
	case "integrate":
		return Integrate(args[1:])
	case "version", "--version", "-v":
		fmt.Printf("agent-runtime %s\n", rtmcp.Version)
		return nil
	case "help", "--help", "-h":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// Serve runs the MCP stdio server until the client disconnects or a shutdown
// signal arrives, then gracefully stops all managed processes.
func Serve(loaded *config.Loaded, logger *slog.Logger) error {
	rt := runtime.New(loaded, logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := rtmcp.NewServer(rt, logger)
	err := server.Run(ctx, &mcp.StdioTransport{})
	if serr := rt.Shutdown(); err == nil {
		err = serr
	}
	return err
}

// Integrate prints (or, with --write, installs) the MCP client configuration
// for the requested agent.
func Integrate(args []string) error {
	// Parse manually: the stdlib flag package stops at the first non-flag
	// argument, which would make "integrate claude --write" (the documented
	// order) treat --write as a positional.
	write := false
	pos := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "--write", "-write":
			write = true
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) != 1 {
		return errors.New("usage: agent-runtime integrate <claude|codex|gemini|opencode|generic> [--write]")
	}
	agent := integrate.Agent(pos[0])
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	if write {
		if agent == integrate.Generic {
			return errors.New("--write is not supported for the generic agent")
		}
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		path, err := integrate.Write(agent, exe, cwd)
		if err != nil {
			return err
		}
		fmt.Printf("wrote agent-runtime config to %s\n", path)
		return nil
	}

	block, err := integrate.Block(agent, exe)
	if err != nil {
		return err
	}
	fmt.Print(block)
	fmt.Printf("\n# To install: agent-runtime integrate %s --write\n", agent)
	return nil
}

func usage() {
	fmt.Print(`agent-runtime - local async process supervisor for AI coding agents.

Usage:
  agent-runtime [serve]            Run the MCP server over stdio (default).
  agent-runtime run <cmd> [args]   Start a process in the foreground and tail its output.
  agent-runtime run --app <name>   Start a named app from agent-runtime.yaml.
  agent-runtime integrate <agent>  Print MCP config for claude|codex|gemini|opencode|generic.
  agent-runtime integrate <agent> --write
                                   Write the config into the agent's config file.
  agent-runtime version
`)
}
