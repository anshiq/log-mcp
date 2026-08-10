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
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	app := fs.String("app", "", "named app from agent-runtime.yaml")
	workdir := fs.String("workdir", "", "working directory override")
	if err := fs.Parse(args); err != nil {
		return err
	}

	req := api.StartRequest{App: *app, WorkDir: *workdir}
	if pos := fs.Args(); len(pos) > 0 {
		req.Command = pos[0]
		req.Args = pos[1:]
	}

	rt := runtime.New(loaded, logger)
	res, err := rt.Start(context.Background(), req)
	if err != nil {
		return err
	}
	fmt.Printf("process_id=%s instance_id=%s status=%s profile=%s\n",
		res.ProcessID, res.InstanceID, res.Status, res.Profile)
	fmt.Printf("command: %s %v (workdir %s)\n\n", res.Command, res.Args, res.WorkDir)

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

	tail(proc, os.Stdout, ctx)
	<-stopDone
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

// tail prints new log entries until ctx is done.
func tail(proc *process.ManagedProcess, w io.Writer, ctx context.Context) {
	// From is inclusive; advance the cursor past the last printed entry.
	next := proc.EntryFrom()
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			entries := proc.Logs.From(next)
			if len(entries) == 0 {
				continue
			}
			for _, e := range entries {
				next = e.ID + 1
				prefix := "stdout"
				if e.Stream == logs.StreamStderr {
					prefix = "stderr"
				}
				fmt.Fprintf(w, "[%s] %s\n", prefix, e.Line)
			}
		}
	}
}
