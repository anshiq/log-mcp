// Cobra command tree for agent-runtime. The MCP server ("serve") is an
// explicit subcommand; running with no arguments prints the command help.
// run/shell/integrate keep their hand-rolled flag parsers (DisableFlagParsing)
// so flags may appear after positional arguments.
package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"agent-runtime/internal/config"
	rtmcp "agent-runtime/internal/mcp"
)

// Run is the CLI entry point invoked from main with os.Args[1:].
//
// No arguments -> the command help. The process manager lives under "repl";
// "serve" and the other subcommands run as one-shots. The REPL commands
// (start/logs/wait/...) only exist inside a session because they manage the
// session's processes.
func Run(args []string, logger *slog.Logger) error {
	cwd, _ := os.Getwd()
	loaded := config.EmptyLoaded(cwd)
	if len(args) == 0 {
		return runBare(loaded, logger)
	}
	for _, a := range args {
		if a == "--no-gui" {
			return newRootCmd(nil, loaded, logger).Help()
		}
		if a == "--tui" || a == "-t" {
			return errors.New("the TUI has been removed; use `agent-runtime web` for the web interface")
		}
		if a == "--gui" {
			return errors.New("the desktop GUI has been removed; use `agent-runtime web` for the web interface")
		}
	}
	root := newRootCmd(nil, loaded, logger)
	root.SetArgs(args)
	return root.Execute()
}

func runBare(loaded *config.Loaded, logger *slog.Logger) error {
	return newRootCmd(nil, loaded, logger).Help()
}

// newRootCmd builds the full command tree. s is nil for one-shot use; when
// non-nil, the session-scoped process commands are added (interactive REPL).
func newRootCmd(s *session, loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	var showVersion bool
	root := &cobra.Command{
		Use:   "agent-runtime",
		Short: "local async process supervisor for AI coding agents",
		Long: `agent-runtime supervises development processes and exposes them over MCP.

Run with no arguments to print the command help.

One-shot commands:
   web                Open the web UI (token URL + browser).
   serve              Run the MCP stdio server (what coding agents launch).
   repl               Open the interactive process manager (start/logs/wait/stop).
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
		newDaemonCmd(loaded, logger),
		newProjectCmd(loaded, logger),
		newDaemonPsCmd(loaded, logger),
		newDaemonLogsCmd(loaded, logger),
		newDaemonStartCmd(loaded, logger),
		newDaemonStopCmd(loaded, logger),
		newDaemonRestartCmd(loaded, logger),
		newDaemonConfigCmd(loaded, logger),
		newDaemonSessionsCmd(loaded, logger),
		newMigrateCmd(loaded, logger),
		newDoctorCmd(loaded, logger),
		newWebCmd(loaded, logger),
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
	var httpAddr string
	var embedded bool
	var project string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the MCP server over stdio (or --http)",
		Long:  "Run the MCP server as a thin client of the per-user daemon (started on demand). With --embedded, run the legacy session-scoped runtime instead. With --http ADDR, serve over the streamable-HTTP transport instead (requires runtime.http.token).",
		RunE: func(cmd *cobra.Command, args []string) error {
			if httpAddr != "" {
				return ServeHTTP(loaded, logger, httpAddr)
			}
			if embedded || project != "" {
				return serveStdio(loaded, logger, serveOptions{embedded: embedded, project: project})
			}
			return Serve(loaded, logger)
		},
	}
	cmd.Flags().StringVar(&httpAddr, "http", "", "serve over streamable-HTTP on this address (e.g. :7341) instead of stdio; requires runtime.http.token")
	cmd.Flags().BoolVar(&embedded, "embedded", false, "legacy session-scoped runtime instead of the daemon bridge (removed in v3.2)")
	cmd.Flags().StringVar(&project, "project", "", "workspace path or id to serve (default: current project)")
	return cmd
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
