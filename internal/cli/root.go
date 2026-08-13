// Cobra command tree for agent-runtime. The MCP server ("serve") is an
// explicit subcommand; running with no arguments opens the interactive process
// manager (repl.go). run/shell/integrate keep their hand-rolled flag parsers
// (DisableFlagParsing) so flags may appear after positional arguments.
package cli

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"agent-runtime/internal/config"
	rtmcp "agent-runtime/internal/mcp"
)

// Run is the CLI entry point invoked from main with os.Args[1:].
//
// No arguments -> the interactive setup wizard (menu: init agent-runtime.yaml,
// install skills, connect an AI agent, open the process manager). The process
// manager itself lives under "repl"; "serve" and the other subcommands run as
// one-shots. The REPL commands (start/logs/wait/...) only exist inside a
// session because they manage the session's processes.
func Run(args []string, logger *slog.Logger) error {
	loaded, err := config.LoadDefault()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return setupMain(loaded, logger)
	}
	root := newRootCmd(nil, loaded, logger)
	root.SetArgs(args)
	return root.Execute()
}

// newRootCmd builds the full command tree. s is nil for one-shot use; when
// non-nil, the session-scoped process commands are added (interactive REPL).
func newRootCmd(s *session, loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	var showVersion bool
	root := &cobra.Command{
		Use:   "agent-runtime",
		Short: "local async process supervisor for AI coding agents",
		Long: `agent-runtime supervises development processes and exposes them over MCP.

Run with no arguments for the interactive setup wizard (init agent-runtime.yaml,
install or remove skills, connect or disconnect an AI coding agent).

One-shot commands:
  serve                Run the MCP stdio server (what coding agents launch).
  repl                 Open the interactive process manager (start/logs/wait/stop).
  run <cmd> [args...]  Start a process in the foreground and tail its output.
  shell <app|proc>     Start an interactive shell in a process's environment.
  integrate <agent>    Print, install (--write) or remove (--remove) MCP config.
  integrate skill      Install (--write) or remove (--remove) embedded skills.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				fmt.Printf("agent-runtime %s\n", rtmcp.Version)
				return nil
			}
			return cmd.Help()
		},
	}
	root.PersistentFlags().BoolVarP(&showVersion, "version", "v", false, "print version and exit")
	root.CompletionOptions.DisableDefaultCmd = true

	root.AddCommand(
		newServeCmd(loaded, logger),
		newReplCmd(loaded, logger),
		newRunCmd(loaded, logger),
		newShellCmd(loaded, logger),
		newIntegrateCmd(loaded, logger),
		newVersionCmd(),
	)
	if s != nil {
		root.AddCommand(newServeHintCmd())
		root.AddCommand(s.sessionCommands()...)
	}
	return root
}

// newReplCmd opens the interactive process manager. Its commands map 1:1 to
// the MCP tools (start/ps/logs/wait/signal/stop/env/shell/exit, ...).
func newReplCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "repl",
		Short: "Open the interactive process manager",
		Long:  "Open the interactive process manager: start/stop/signal processes, read and wait on logs, inspect env, and shell into a process's environment.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return replMain(loaded, logger)
		},
	}
}

func newServeCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the MCP server over stdio",
		Long:  "Run the MCP stdio server until the client disconnects or a shutdown signal arrives, then gracefully stop all managed processes.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return Serve(loaded, logger)
		},
	}
}

func newRunCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:                "run <command> [args...]",
		Short:              "Start a process in the foreground and tail its output",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunCommand(args, loaded, logger)
		},
	}
}

func newShellCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:                "shell <app|process_id> [--shell path]",
		Short:              "Start an interactive shell in a process's environment",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return ShellCommand(args, loaded, logger)
		},
	}
}

func newIntegrateCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:                "integrate <agent|skill> [--write|--remove] [flags]",
		Short:              "Print, install or remove MCP client config / embedded skills",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return Integrate(args)
		},
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("agent-runtime %s\n", rtmcp.Version)
			return nil
		},
	}
}

// newServeHintCmd is the REPL-only placeholder for `serve`: the stdio server
// must own stdin, so it cannot run inside the session.
func newServeHintCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the MCP stdio server (not available inside the REPL)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("serve owns stdin and cannot run inside this session; exit the REPL and run: agent-runtime serve")
		},
	}
}
