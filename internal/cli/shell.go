package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"agent-runtime/internal/config"
	"agent-runtime/internal/runtime"
	"agent-runtime/pkg/api"
)

// ShellCommand starts an interactive shell inside the environment (resolved
// workdir + full env) of a running process or a configured app, forwards stdin
// to it, and tails its output until interrupted, then stops it.
func ShellCommand(args []string, loaded *config.Loaded, logger *slog.Logger) error {
	fs := flag.NewFlagSet("shell", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: agent-runtime shell <process_id|app> [--shell path]\n")
		fmt.Fprintf(fs.Output(), "  target is tried as a process_id (proc_<hex>) first, then as an app name from agent-runtime.yaml\n")
		fs.PrintDefaults()
	}
	shell := fs.String("shell", "", "shell executable (default $SHELL, then /bin/sh)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pos := fs.Args()
	if len(pos) != 1 {
		fs.Usage()
		return fmt.Errorf("usage: agent-runtime shell <process_id|app> [--shell path]: got %d target(s), want exactly one", len(pos))
	}
	target := pos[0]

	rt := runtime.New(loaded, logger)
	res, err := rt.OpenShell(context.Background(), api.OpenShellRequest{
		ProcessID: target,
		App:       target,
		Shell:     *shell,
	})
	if err != nil {
		return err
	}
	fmt.Printf("process_id=%s instance_id=%s status=%s profile=%s\n",
		res.ProcessID, res.InstanceID, res.Status, res.Profile)
	fmt.Printf("shell: %s %v (workdir %s)\n\n", res.Command, res.Args, res.WorkDir)

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
	go forwardStdin(rt, res.ProcessID, ctx)

	next := proc.EntryFrom()
	tail(proc, os.Stdout, ctx, &next)
	<-stopDone
	return nil
}

// forwardStdin copies lines from os.Stdin to the process's stdin until ctx is
// done or stdin reaches EOF. bufio.Scanner strips the trailing newline, so each
// line is re-terminated with "\n" before being sent. Write errors are ignored:
// they only occur once the process is gone.
func forwardStdin(rt *runtime.Runtime, processID string, ctx context.Context) {
	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-lines:
			if !ok {
				return
			}
			rt.SendStdin(processID, line+"\n")
		}
	}
}
