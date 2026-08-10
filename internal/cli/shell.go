package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"agent-runtime/internal/config"
	"agent-runtime/internal/runtime"
	"agent-runtime/pkg/api"
)

// ShellCommand starts an interactive shell inside the environment (resolved
// workdir + full env) of a running process or a configured app, forwards stdin
// to it, and tails its output until interrupted, then stops it.
func ShellCommand(args []string, loaded *config.Loaded, logger *slog.Logger) error {
	shell, pos, err := parseShellArgs(args)
	if err != nil {
		printShellUsage()
		return err
	}
	if len(pos) != 1 {
		printShellUsage()
		return fmt.Errorf("usage: agent-runtime shell <app> [--shell path]: got %d target(s), want exactly one", len(pos))
	}
	target := pos[0]

	rt := runtime.New(loaded, logger)
	procID, app := shellTarget(target)
	res, err := rt.OpenShell(context.Background(), api.OpenShellRequest{
		ProcessID: procID,
		App:       app,
		Shell:     shell,
	})
	if err != nil {
		if procID != "" {
			return fmt.Errorf("cannot open shell for process %q: %w\nnote: process registries are per-invocation — a proc_ id from a running `agent-runtime serve`/MCP session is not visible to this CLI command; use the open_shell MCP tool there, or shell into an app by name (agent-runtime shell <app>)", target, err)
		}
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

// printShellUsage prints the shell command usage block to stderr.
func printShellUsage() {
	fmt.Fprintf(os.Stderr, "usage: agent-runtime shell <app> [--shell path]\n")
	fmt.Fprintf(os.Stderr, "  shell into the resolved workdir+env of an app from agent-runtime.yaml\n")
	fmt.Fprintf(os.Stderr, "  (a proc_<hex> process id only resolves inside the same serve session; over MCP use open_shell)\n")
	fmt.Fprintf(os.Stderr, "  --shell path   shell executable (default $SHELL, then /bin/sh)\n")
}

// shellTarget maps a CLI target to exactly one of ProcessID or App for
// OpenShell (which prefers ProcessID when set — passing both would shadow the
// app path). A proc_<hex> string is a process id; anything else is an app
// name from agent-runtime.yaml.
func shellTarget(target string) (procID, app string) {
	if strings.HasPrefix(target, "proc_") {
		return target, ""
	}
	return "", target
}

// parseShellArgs parses the shell flags by hand: the stdlib flag package stops
// at the first non-flag argument, which would make the documented
// "shell <target> --shell path" order treat --shell as a positional. Flags may
// appear before or after the target.
func parseShellArgs(args []string) (shell string, targets []string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--shell" || a == "-shell":
			if i+1 >= len(args) {
				return "", nil, errors.New("--shell requires an argument")
			}
			i++
			shell = args[i]
		case strings.HasPrefix(a, "--shell="):
			shell = strings.TrimPrefix(a, "--shell=")
		case strings.HasPrefix(a, "-shell="):
			shell = strings.TrimPrefix(a, "-shell=")
		case strings.HasPrefix(a, "-"):
			return "", nil, fmt.Errorf("unknown flag %q", a)
		default:
			targets = append(targets, a)
		}
	}
	return shell, targets, nil
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
