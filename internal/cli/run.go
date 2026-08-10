package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"agent-runtime/internal/config"
	"agent-runtime/internal/logs"
	"agent-runtime/internal/process"
	"agent-runtime/internal/runtime"
	"agent-runtime/pkg/api"
)

// RunCommand starts a process in the foreground and tails its output until
// interrupted, then stops it. Useful for debugging the runtime directly.
//
// Unlike MCP mode, the managed process's lifetime is tied to this invocation:
// when the command exits, the process is stopped.
func RunCommand(args []string, loaded *config.Loaded, logger *slog.Logger) error {
	opts, err := parseRunArgs(args)
	if err != nil {
		return err
	}

	req := api.StartRequest{App: opts.app, WorkDir: opts.workdir, Env: opts.env, EnvFile: opts.envFile}
	req.Command = opts.command
	req.Args = opts.args

	rt := runtime.New(loaded, logger)
	res, err := rt.Start(context.Background(), req)
	if err != nil {
		return err
	}
	fmt.Printf("process_id=%s instance_id=%s status=%s profile=%s\n",
		res.ProcessID, res.InstanceID, res.Status, res.Profile)
	fmt.Printf("command: %s %v (workdir %s)\n", res.Command, res.Args, res.WorkDir)
	if len(opts.env) > 0 || opts.envFile != "" {
		fmt.Printf("env: %d override(s), env_file=%s\n", len(opts.env), opts.envFile)
	}
	fmt.Println()

	proc, ok := rt.Manager().Get(res.ProcessID)
	if !ok {
		return fmt.Errorf("internal: process %s not found after start", res.ProcessID)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	stopDone := make(chan struct{})
	go func() {
		defer close(stopDone)
		<-ctx.Done()
		fmt.Fprintf(os.Stderr, "\nstopping %s...\n", res.ProcessID)
		rt.Stop(context.Background(), res.ProcessID)
	}()
	go watchExit(proc, os.Stderr)

	next := proc.EntryFrom()
	tail(proc, os.Stdout, ctx, &next)
	<-stopDone
	// The app may have written its final lines while rt.Stop was running (after
	// tail returned on ctx.Done); drain them now so graceful-shutdown output is
	// shown. drainLogs resumes from the same cursor tail advanced, so no line is
	// printed twice or skipped.
	drainLogs(proc.Logs, os.Stdout, &next)
	return nil
}

// watchExit reports process termination on stderr.
func watchExit(proc *process.ManagedProcess, w io.Writer) {
	<-proc.Done()
	info := proc.Info()
	if info.ExitCode != nil {
		fmt.Fprintf(w, "[runtime] process exited (code=%d)\n", *info.ExitCode)
	} else {
		fmt.Fprintf(w, "[runtime] process exited\n")
	}
}

// tail prints new log entries until ctx is done, then returns so the caller
// can drain anything appended during the graceful stop.
func tail(proc *process.ManagedProcess, w io.Writer, ctx context.Context, next *uint64) {
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			drainLogs(proc.Logs, w, next)
		}
	}
}

// drainLogs prints every entry at or after the *next cursor, advancing it past
// the last printed entry so a later call never reprints a line. Shared by the
// tail tick loop and the post-stop drain in RunCommand.
func drainLogs(l *logs.ProcessLogs, w io.Writer, next *uint64) {
	entries := l.From(*next)
	if len(entries) == 0 {
		return
	}
	for _, e := range entries {
		*next = e.ID + 1
		prefix := "stdout"
		if e.Stream == logs.StreamStderr {
			prefix = "stderr"
		}
		fmt.Fprintf(w, "[%s] %s\n", prefix, e.Line)
	}
}

// stringList is a repeatable flag value: each --env occurrence appends to the
// slice in order.
type stringList []string

// String renders the accumulated values for flag help.
func (l *stringList) String() string {
	return fmt.Sprintf("%v", []string(*l))
}

// Set appends one KEY=VALUE override.
func (l *stringList) Set(v string) error {
	*l = append(*l, v)
	return nil
}

// runOptions is the parsed `run` flagset. Split out of RunCommand so tests can
// exercise the flag wiring without starting a process.
type runOptions struct {
	app     string
	workdir string
	env     stringList
	envFile string
	command string
	args    []string
}

// parseRunArgs parses the `run` flagset. Positional arguments after the flags
// form the raw start command.
func parseRunArgs(args []string) (runOptions, error) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	app := fs.String("app", "", "named app from agent-runtime.yaml")
	workdir := fs.String("workdir", "", "working directory override")
	var env stringList
	fs.Var(&env, "env", "KEY=VALUE environment override (repeatable)")
	envFile := fs.String("env-file", "", "dotenv file layered below --env (resolved relative to the config file)")
	if err := fs.Parse(args); err != nil {
		return runOptions{}, err
	}
	opts := runOptions{app: *app, workdir: *workdir, env: env, envFile: *envFile}
	if pos := fs.Args(); len(pos) > 0 {
		opts.command = pos[0]
		opts.args = pos[1:]
	}
	return opts, nil
}
