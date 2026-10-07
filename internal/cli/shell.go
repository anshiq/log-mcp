package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"agent-runtime/internal/config"
	"agent-runtime/internal/runtime"
	"agent-runtime/pkg/api"
)

// ShellCommand starts an interactive shell inside the environment (resolved
// workdir + full env) of a running process, a configured app, or any process
// on the machine by OS pid (--pid). Forwards stdin to it and tails its output
// until interrupted, then stops it.
func ShellCommand(args []string, loaded *config.Loaded, logger *slog.Logger) error {
	shell, pid, targets, err := parseShellArgs(args)
	if err != nil {
		printShellUsage()
		return err
	}

	var rt *runtime.Runtime
	var res *api.StartResult
	switch {
	case pid > 0:
		if len(targets) > 0 {
			printShellUsage()
			return fmt.Errorf("cannot combine --pid with a target")
		}
		rt = runtime.New(loaded, logger)
		res, err = rt.OpenShell(context.Background(), api.OpenShellRequest{PID: pid, Shell: shell})
		if err != nil {
			return fmt.Errorf("cannot open shell for pid %d: %w", pid, err)
		}
	default:
		if len(targets) != 1 {
			printShellUsage()
			return fmt.Errorf("usage: agent-runtime shell <app> [--shell path]: got %d target(s), want exactly one", len(targets))
		}
		target := targets[0]
		procID, app := shellTarget(target)
		rt = runtime.New(loaded, logger)
		res, err = rt.OpenShell(context.Background(), api.OpenShellRequest{
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
	}
	return runShell(rt, res)
}

// runShell prints the started shell's identity, forwards stdin to it, tails
// its output until interrupted, then stops it.
func runShell(rt *runtime.Runtime, res *api.StartResult) error {
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
	fmt.Fprintf(os.Stderr, "       agent-runtime shell --pid <os-pid> [--shell path]\n")
	fmt.Fprintf(os.Stderr, "  shell into the resolved workdir+env of an app from the project config\n")
	fmt.Fprintf(os.Stderr, "  (a proc_<hex> process id only resolves inside the same serve session; over MCP use open_shell)\n")
	fmt.Fprintf(os.Stderr, "  --shell path   shell executable (default $SHELL, then /bin/sh)\n")
	fmt.Fprintf(os.Stderr, "  --pid <os-pid>   attach to a process by OS pid (reads /proc/<pid>/cwd + environ; works for processes from other agent-runtime sessions)\n")
}

// shellTarget maps a CLI target to exactly one of ProcessID or App for
// OpenShell (which prefers ProcessID when set — passing both would shadow the
// app path). A proc_<hex> string is a process id; anything else is an app
// name from the project config.
func shellTarget(target string) (procID, app string) {
	if strings.HasPrefix(target, "proc_") {
		return target, ""
	}
	return "", target
}

// parseShellArgs parses the shell flags by hand: the stdlib flag package stops
// at the first non-flag argument, which would make the documented
// "shell <target> --shell path" order treat --shell as a positional. Flags may
// appear before or after the target. --pid carries the OS pid of a process to
// attach to and is mutually exclusive with a target at the ShellCommand level.
func parseShellArgs(args []string) (shell string, pid int, targets []string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--shell" || a == "-shell":
			if i+1 >= len(args) {
				return "", 0, nil, errors.New("--shell requires an argument")
			}
			i++
			shell = args[i]
		case strings.HasPrefix(a, "--shell="):
			shell = strings.TrimPrefix(a, "--shell=")
		case strings.HasPrefix(a, "-shell="):
			shell = strings.TrimPrefix(a, "-shell=")
		case a == "--pid" || a == "-pid":
			if i+1 >= len(args) {
				return "", 0, nil, errors.New("--pid requires an argument")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil {
				return "", 0, nil, fmt.Errorf("invalid --pid value %q", args[i])
			}
			pid = n
		case strings.HasPrefix(a, "--pid="):
			v := strings.TrimPrefix(a, "--pid=")
			n, err := strconv.Atoi(v)
			if err != nil {
				return "", 0, nil, fmt.Errorf("invalid --pid value %q", v)
			}
			pid = n
		case strings.HasPrefix(a, "-pid="):
			v := strings.TrimPrefix(a, "-pid=")
			n, err := strconv.Atoi(v)
			if err != nil {
				return "", 0, nil, fmt.Errorf("invalid --pid value %q", v)
			}
			pid = n
		case strings.HasPrefix(a, "-"):
			return "", 0, nil, fmt.Errorf("unknown flag %q", a)
		default:
			targets = append(targets, a)
		}
	}
	return shell, pid, targets, nil
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
