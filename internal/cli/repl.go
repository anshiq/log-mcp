// Interactive process manager. `agent-runtime` with no arguments opens a REPL
// that drives the runtime facade directly (no MCP): start/stop/signal
// processes, read and wait on logs, inspect env, and shell into a process's
// environment. Every command maps 1:1 to an MCP tool, so what you learn here
// is exactly what a coding agent sees through the server.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"agent-runtime/internal/config"
	rtmcp "agent-runtime/internal/mcp"
	"agent-runtime/internal/runtime"
	"agent-runtime/pkg/api"
)

// errExitREPL signals a clean REPL exit via the exit/quit command.
var errExitREPL = errors.New("exit interactive session")

// session is the persistent runtime context for the interactive REPL. It owns
// one runtime; every process the user starts lives in it and is visible to
// ps/status/logs/etc.
type session struct {
	rt     *runtime.Runtime
	loaded *config.Loaded
}

// replMain runs the interactive session. On non-interactive stdin (no TTY) it
// prints help instead. The session runtime is shut down on exit so managed
// processes stop gracefully. Ctrl-C cancels the in-flight command when one is
// running, or exits the session when idle; Ctrl-D exits.
func replMain(loaded *config.Loaded, logger *slog.Logger) error {
	if !isTTY(os.Stdin) {
		root := newRootCmd(nil, loaded, logger)
		return root.Help()
	}
	rt := runtime.New(loaded, logger)
	defer func() { _ = rt.Shutdown() }()
	s := &session{rt: rt, loaded: loaded}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	banner(s)

	reader := bufio.NewReader(os.Stdin)
	lineCh := make(chan string)
	errCh := make(chan error, 1)
	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				errCh <- err
				return
			}
			lineCh <- line
		}
	}()

	for {
		fmt.Print("agent-runtime> ")
		select {
		case <-sigCh:
			fmt.Println("\ninterrupted")
			return nil
		case <-errCh:
			fmt.Println()
			return nil // EOF (Ctrl-D) or read error
		case line := <-lineCh:
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			fields := strings.Fields(line)
			root := newRootCmd(s, loaded, logger)
			// In-flight commands get a context cancelled by the next SIGINT, so
			// Ctrl-C aborts a blocking wait/stop without killing the session.
			cmdCtx, cmdCancel := context.WithCancel(context.Background())
			go func() {
				select {
				case <-sigCh:
					cmdCancel()
				case <-cmdCtx.Done():
				}
			}()
			root.SetContext(cmdCtx)
			root.SetArgs(fields)
			execErr := root.Execute()
			cmdCancel()
			switch {
			case errors.Is(execErr, errExitREPL):
				return nil
			case errors.Is(execErr, context.Canceled):
				fmt.Println("interrupted")
			case execErr != nil:
				fmt.Fprintf(os.Stderr, "error: %v\n", execErr)
			}
		}
	}
}

func banner(s *session) {
	fmt.Printf("agent-runtime %s interactive process manager\n", rtmcp.Version)
	if len(s.loaded.Config.Apps) > 0 {
		fmt.Println("configured apps:")
		for _, n := range s.loaded.Names() {
			fmt.Printf("  %s\n", n)
		}
	}
	fmt.Println("type 'help' for commands, 'exit' (or Ctrl-D) to quit")
}

// sessionCommands returns the process-management commands bound to the session
// runtime. Each maps to the same runtime call an MCP tool makes.
func (s *session) sessionCommands() []*cobra.Command {
	return []*cobra.Command{
		newStartCmd(s),
		newListCmd(s),
		newAppsCmd(s),
		newStatusCmd(s),
		newLogsCmd(s),
		newWaitCmd(s),
		newWaitForExitCmd(s),
		newSignalCmd(s),
		newStopCmd(s),
		newRestartCmd(s),
		newRemoveCmd(s),
		newEnvCmd(s),
		newSendCmd(s),
		newClearCmd(s),
		newShellInCmd(s),
		newExitCmd(),
	}
}

func (s *session) isApp(name string) bool {
	_, ok := s.loaded.Config.Apps[name]
	return ok
}

func newStartCmd(s *session) *cobra.Command {
	return &cobra.Command{
		Use:                "start <command> [args...]",
		Short:              "Start a process and return its process_id (never blocks)",
		DisableFlagParsing: true,
		Long: `Start a supervised process and return its process_id immediately.
  start --app <name> [--env K=V ...]     start a named app from the project config
  start <command> [args...]              start a raw command
  start <app-name>                       shortcut: a lone arg matching a configured app
Flags must precede the command (mirrors 'run').`,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := parseRunArgs(args)
			if err != nil {
				return err
			}
			req := api.StartRequest{App: opts.app, WorkDir: opts.workdir, Env: opts.env, EnvFile: opts.envFile}
			if req.App == "" {
				if opts.command == "" {
					return errors.New("start needs a command (or --app <name>)")
				}
				if len(opts.args) == 0 && s.isApp(opts.command) {
					req.App = opts.command
				} else {
					req.Command = opts.command
					req.Args = opts.args
				}
			}
			res, err := s.rt.Start(cmd.Context(), req)
			if err != nil {
				return err
			}
			printStart(res)
			return nil
		},
	}
}

func newListCmd(s *session) *cobra.Command {
	return &cobra.Command{
		Use:     "ps",
		Aliases: []string{"list", "processes"},
		Short:   "List managed processes",
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := s.rt.List()
			if err != nil {
				return err
			}
			printProcesses(res.Processes)
			return nil
		},
	}
}

func newAppsCmd(s *session) *cobra.Command {
	return &cobra.Command{
		Use:   "apps",
		Short: "List apps configured in the project config",
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := s.rt.Apps()
			if err != nil {
				return err
			}
			printApps(res.Apps)
			return nil
		},
	}
}

func newStatusCmd(s *session) *cobra.Command {
	return &cobra.Command{
		Use:   "status <process_id>",
		Short: "Show process lifecycle status, pid, and exit code",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := s.rt.Status(args[0])
			if err != nil {
				return err
			}
			printStatus(res)
			return nil
		},
	}
}

func newLogsCmd(s *session) *cobra.Command {
	var stream string
	var lines int
	var contains string
	cmd := &cobra.Command{
		Use:   "logs <process_id>",
		Short: "Read captured stdout/stderr lines",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := s.rt.GetLogs(api.GetLogsRequest{
				ProcessID: args[0], Stream: stream, Lines: lines, Contains: contains,
			})
			if err != nil {
				return err
			}
			printLogs(res)
			return nil
		},
	}
	cmd.Flags().StringVar(&stream, "stream", "all", "all|stdout|stderr")
	cmd.Flags().IntVar(&lines, "lines", 100, "maximum lines to read")
	cmd.Flags().StringVar(&contains, "contains", "", "only lines containing this substring")
	return cmd
}

func newWaitCmd(s *session) *cobra.Command {
	var ready bool
	var contains, pattern string
	var timeoutMS int
	cmd := &cobra.Command{
		Use:   "wait <process_id>",
		Short: "Wait until a log line matches, the process exits, or timeout",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := s.rt.WaitForLog(cmd.Context(), api.WaitForLogRequest{
				ProcessID: args[0], Ready: ready, Contains: contains, Pattern: pattern, TimeoutMS: timeoutMS,
			})
			if err != nil {
				return err
			}
			printWait(res)
			return nil
		},
	}
	cmd.Flags().BoolVar(&ready, "ready", false, "use the app profile's readiness pattern")
	cmd.Flags().StringVar(&contains, "contains", "", "match lines containing this substring")
	cmd.Flags().StringVar(&pattern, "pattern", "", "match lines by regular expression")
	cmd.Flags().IntVar(&timeoutMS, "timeout", 30000, "timeout in ms (default 30000)")
	return cmd
}

func newWaitForExitCmd(s *session) *cobra.Command {
	var timeoutMS int
	cmd := &cobra.Command{
		Use:     "waitfor <process_id>",
		Aliases: []string{"wait-exit"},
		Short:   "Wait until the process exits or timeout",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := s.rt.WaitForExit(cmd.Context(), api.WaitForExitParams{ProcessID: args[0], TimeoutMS: timeoutMS})
			if err != nil {
				return err
			}
			switch {
			case res.Exited:
				code := "?"
				if res.ExitCode != nil {
					code = strconv.Itoa(*res.ExitCode)
				}
				fmt.Printf("exited (code=%s)\n", code)
			case res.Timeout:
				fmt.Println("timeout: process still running")
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&timeoutMS, "timeout", 30000, "timeout in ms (default 30000)")
	return cmd
}

func newSignalCmd(s *session) *cobra.Command {
	return &cobra.Command{
		Use:   "signal <process_id> <SIG>",
		Short: "Deliver a signal to the whole process group (SIGINT|SIGTERM|SIGHUP|SIGQUIT|SIGUSR1|SIGUSR2|SIGKILL)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := s.rt.SignalProcess(cmd.Context(), api.SignalProcessRequest{ProcessID: args[0], Signal: args[1]})
			if err != nil {
				return err
			}
			fmt.Printf("signaled %s (%s)\n", res.ProcessID, res.Signal)
			return nil
		},
	}
}

func newStopCmd(s *session) *cobra.Command {
	return &cobra.Command{
		Use:   "stop <process_id>",
		Short: "Stop a process (SIGTERM, then SIGKILL after the grace period)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := s.rt.Stop(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Printf("stopped %s\n", args[0])
			return nil
		},
	}
}

func newRestartCmd(s *session) *cobra.Command {
	return &cobra.Command{
		Use:   "restart <process_id>",
		Short: "Stop and relaunch a process (same process_id, log history preserved)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := s.rt.Restart(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Printf("restarted %s\n", args[0])
			return nil
		},
	}
}

func newRemoveCmd(s *session) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "remove <process_id>",
		Short: "Remove a process from the registry and free its log buffers",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := s.rt.RemoveProcess(args[0], force); err != nil {
				return err
			}
			fmt.Printf("removed %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "stop a running process before removing it")
	return cmd
}

func newEnvCmd(s *session) *cobra.Command {
	var live, reveal bool
	cmd := &cobra.Command{
		Use:   "env <process_id>",
		Short: "Show a process's complete merged environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := s.rt.ProcessEnv(api.ProcessEnvRequest{ProcessID: args[0], Live: live, Reveal: reveal})
			if err != nil {
				return err
			}
			printEnv(res)
			return nil
		},
	}
	cmd.Flags().BoolVar(&live, "live", false, "read the real env from /proc/<pid>/environ")
	cmd.Flags().BoolVar(&reveal, "reveal", false, "show secret values instead of redacting them")
	return cmd
}

func newSendCmd(s *session) *cobra.Command {
	return &cobra.Command{
		Use:   "send <process_id> <text...>",
		Short: "Write a line to the process's stdin",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := s.rt.SendStdin(args[0], strings.Join(args[1:], " ")+"\n")
			if err != nil {
				return err
			}
			fmt.Printf("wrote %d bytes\n", res.Written)
			return nil
		},
	}
}

func newClearCmd(s *session) *cobra.Command {
	var stream string
	cmd := &cobra.Command{
		Use:   "clear <process_id>",
		Short: "Empty a process's log buffer",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := s.rt.ClearLogs(args[0], stream)
			if err != nil {
				return err
			}
			fmt.Printf("cleared %s (%d entries removed)\n", res.ProcessID, res.Removed)
			return nil
		},
	}
	cmd.Flags().StringVar(&stream, "stream", "all", "all|stdout|stderr")
	return cmd
}

func newShellInCmd(s *session) *cobra.Command {
	var shell string
	cmd := &cobra.Command{
		Use:   "shell <app|process_id>",
		Short: "Start a shell inside a process's resolved workdir+env",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := api.OpenShellRequest{Shell: shell}
			if strings.HasPrefix(args[0], "proc_") {
				req.ProcessID = args[0]
			} else {
				req.App = args[0]
			}
			res, err := s.rt.OpenShell(cmd.Context(), req)
			if err != nil {
				return err
			}
			printStart(res)
			fmt.Println("drive it with 'send <process_id> ...' and read it with 'logs <process_id>'")
			return nil
		},
	}
	cmd.Flags().StringVar(&shell, "shell", "", "shell executable (default $SHELL, then /bin/sh)")
	return cmd
}

func newExitCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "exit",
		Aliases: []string{"quit"},
		Short:   "Exit the interactive session",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errExitREPL
		},
	}
}

func printStart(res *api.StartResult) {
	fmt.Printf("process_id=%s  instance_id=%s  status=%s", res.ProcessID, res.InstanceID, res.Status)
	if res.Profile != "" {
		fmt.Printf("  profile=%s", res.Profile)
	}
	fmt.Println()
	fmt.Printf("command: %s %s\n", res.Command, strings.Join(res.Args, " "))
	fmt.Printf("workdir: %s\n", res.WorkDir)
}

func printProcesses(procs []api.ProcessSummary) {
	if len(procs) == 0 {
		fmt.Println("no processes (use 'start' or 'start --app <name>')")
		return
	}
	fmt.Printf("%-24s %-12s %-10s %s\n", "process_id", "status", "profile", "command")
	for _, p := range procs {
		fmt.Printf("%-24s %-12s %-10s %s\n", p.ProcessID, p.Status, p.Profile, p.Command)
	}
}

func printApps(apps []api.AppInfo) {
	if len(apps) == 0 {
		fmt.Println("no apps configured (add an apps: block in the web Config page)")
		return
	}
	fmt.Printf("%-16s %-12s %s\n", "name", "type", "workdir")
	for _, a := range apps {
		fmt.Printf("%-16s %-12s %s\n", a.Name, a.Type, a.WorkDir)
	}
}

func printStatus(res *api.StatusResult) {
	fmt.Printf("process_id: %s\n", res.ProcessID)
	if res.InstanceID != "" {
		fmt.Printf("instance_id: %s\n", res.InstanceID)
	}
	fmt.Printf("status: %s\n", res.Status)
	if res.PID > 0 {
		fmt.Printf("pid: %d\n", res.PID)
	}
	fmt.Printf("command: %s %s\n", res.Command, strings.Join(res.Args, " "))
	fmt.Printf("workdir: %s\n", res.WorkDir)
	if res.Profile != "" {
		fmt.Printf("profile: %s\n", res.Profile)
	}
	if res.StartedAt != "" {
		fmt.Printf("started: %s\n", res.StartedAt)
	}
	if res.ExitedAt != nil {
		fmt.Printf("exited: %s\n", *res.ExitedAt)
	}
	if res.ExitCode != nil {
		fmt.Printf("exit_code: %d\n", *res.ExitCode)
	}
	if res.Restarts > 0 {
		fmt.Printf("restarts: %d\n", res.Restarts)
	}
	fmt.Printf("log lines: %d stdout / %d stderr\n", res.StdoutLines, res.StderrLines)
}

func printLogs(res *api.GetLogsResult) {
	for _, e := range res.Entries {
		fmt.Printf("[%s] %s\n", e.Stream, e.Line)
	}
	if res.Truncated {
		fmt.Fprintf(os.Stderr, "# truncated: showing %d of %d available lines (source: %s)\n", res.ReturnedLines, res.AvailableLines, res.Source)
	}
}

func printWait(res *api.WaitForLogResult) {
	switch {
	case res.Matched:
		fmt.Printf("matched: [%s] %s\n", res.Entry.Stream, res.Entry.Line)
	case res.Exited:
		fmt.Println("process exited before a matching line appeared")
	case res.Timeout:
		fmt.Println("timeout: no matching line appeared")
	}
}

func printEnv(res *api.ProcessEnvResult) {
	for _, kv := range res.Env {
		fmt.Println(kv)
	}
	if res.Source != nil {
		fmt.Println("# provenance (spec mode): key -> winning layer")
		for _, k := range sortedKeys(res.Source) {
			fmt.Printf("  %s -> %s\n", k, res.Source[k])
		}
	}
	if res.Redacted > 0 {
		fmt.Printf("# %d secret-like value(s) redacted; pass --reveal to show\n", res.Redacted)
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
