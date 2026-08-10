// Package cli implements the agent-runtime command-line interface. The primary
// mode is the stdio MCP server ("serve"); run/integrate/version are debugging
// and setup helpers.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

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
	opts, pos, err := parseIntegrateArgs(args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: agent-runtime integrate <claude|codex|gemini|opencode|generic> [--write] [--scope global|project] [--yes] [--create] [--no-verify]")
	}
	agent := integrate.Agent(pos[0])
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	if agent == integrate.OpenCode {
		return integrateOpenCode(opts, exe)
	}
	if !opts.write {
		block, err := integrate.Block(agent, exe)
		if err != nil {
			return err
		}
		fmt.Print(block)
		fmt.Printf("\n# To install: agent-runtime integrate %s --write\n", agent)
		return nil
	}
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

// integrateOptions holds the parsed flags for `integrate`.
type integrateOptions struct {
	write        bool
	yes          bool
	create       bool
	noVerify     bool
	scope        string
	scopePending bool
}

// parseIntegrateArgs parses the integrate flags by hand: the stdlib flag
// package stops at the first non-flag argument, which would make the documented
// "integrate <agent> --write" order treat --write as a positional. Flags may
// appear before or after the agent name.
func parseIntegrateArgs(args []string) (integrateOptions, []string, error) {
	opts := integrateOptions{}
	pos := make([]string, 0, len(args))
	for _, a := range args {
		switch {
		case a == "--write" || a == "-write":
			opts.write = true
		case a == "--yes" || a == "-y":
			opts.yes = true
		case a == "--create" || a == "-c":
			opts.create = true
		case a == "--no-verify":
			opts.noVerify = true
		case a == "--scope":
			opts.scopePending = true
		case opts.scopePending:
			opts.scope = a
			opts.scopePending = false
		case strings.HasPrefix(a, "--scope="):
			opts.scope = strings.TrimPrefix(a, "--scope=")
		default:
			pos = append(pos, a)
		}
	}
	if opts.scopePending {
		return opts, nil, errors.New("--scope requires an argument (global|project)")
	}
	return opts, pos, nil
}

// integrateOpenCode runs the interactive (or, with --yes, scripted) install
// flow for opencode. It discovers the real config locations, asks the user
// where to install, merges surgically, and verifies opencode can connect.
func integrateOpenCode(opts integrateOptions, exe string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if !opts.write {
		block, err := integrate.Block(integrate.OpenCode, exe)
		if err != nil {
			return err
		}
		fmt.Print(block)
		fmt.Println("\n# To install interactively: agent-runtime integrate opencode --write")
		return nil
	}

	interactive := opts.yes == false && isTTY(os.Stdin)
	cands := integrate.DiscoverOpenCode(cwd)
	existing := existingCandidates(cands)
	if opts.scope != "" {
		if err := validateScope(opts.scope); err != nil {
			return err
		}
		existing = existingCandidates(filterScope(cands, integrate.Scope(opts.scope)))
	}

	var target integrate.Candidate
	switch {
	case len(existing) == 0 && !interactive:
		return fmt.Errorf("no opencode config found; pass --scope global (or project) with --create to create one:\n  %s", describeCandidates(cands))
	case len(existing) == 0:
		target, err = pickCreateLocation(cands)
		if err != nil {
			return err
		}
	case len(existing) > 1 && !interactive:
		return fmt.Errorf("multiple opencode configs found; pick one with --scope %s", scopesOf(existing))
	case len(existing) > 1:
		target, err = pickExisting(cands, existing)
		if err != nil {
			return err
		}
	default:
		target = existing[0]
	}

	if !target.Exists {
		if !interactive && !opts.create {
			return fmt.Errorf("%s does not exist; pass --create to create it", target.Path)
		}
		if interactive {
			ok, err := confirmCreate(target.Path)
			if err != nil {
				return err
			}
			if !ok {
				return errors.New("install cancelled")
			}
		}
		if err := os.MkdirAll(filepath.Dir(target.Path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target.Path, []byte("{\n  \"$schema\": \""+integrate.OpenCodeSchema+"\"\n}\n"), 0o644); err != nil {
			return err
		}
	}

	if interactive {
		showPlan(target, exe)
		ok, err := confirm("Proceed? [Y/n]", true)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("install cancelled")
		}
	}

	changed, err := integrate.MergeOpenCodeConfig(target.Path, []string{exe, "serve"})
	if err != nil {
		return err
	}
	if !changed {
		fmt.Printf("agent-runtime is already configured in %s\n", target.Path)
		return nil
	}
	fmt.Printf("installed agent-runtime into %s\n", target.Path)

	if !opts.noVerify {
		return verifyOpenCode(target.Path)
	}
	return nil
}

func existingCandidates(cands []integrate.Candidate) []integrate.Candidate {
	var out []integrate.Candidate
	for _, c := range cands {
		if c.Exists {
			out = append(out, c)
		}
	}
	return out
}

func filterScope(cands []integrate.Candidate, scope integrate.Scope) []integrate.Candidate {
	var out []integrate.Candidate
	for _, c := range cands {
		if c.Scope == scope {
			out = append(out, c)
		}
	}
	return out
}

func scopesOf(cands []integrate.Candidate) string {
	set := map[integrate.Scope]bool{}
	for _, c := range cands {
		set[c.Scope] = true
	}
	var scopes []string
	for s := range set {
		scopes = append(scopes, string(s))
	}
	return strings.Join(scopes, "|")
}

func validateScope(s string) error {
	if s != string(integrate.ScopeGlobal) && s != string(integrate.ScopeProject) {
		return fmt.Errorf("unknown scope %q (want global or project)", s)
	}
	return nil
}

func describeCandidates(cands []integrate.Candidate) string {
	var sb strings.Builder
	for _, c := range cands {
		state := "missing"
		if c.Exists {
			state = "exists"
		}
		fmt.Fprintf(&sb, "  [%s] %s (%s)\n", c.Scope, c.Path, state)
	}
	return sb.String()
}

func pickExisting(cands, existing []integrate.Candidate) (integrate.Candidate, error) {
	fmt.Println("Existing opencode config(s):")
	for i, c := range existing {
		fmt.Printf("  %d) %s  (%s)\n", i+1, c.Path, c.Scope)
	}
	fmt.Println("  q) quit")
	choice, err := prompt("\nChoose where to install [1-%d]: ", len(existing))
	if err != nil {
		return integrate.Candidate{}, err
	}
	if choice == "q" || choice == "Q" {
		return integrate.Candidate{}, errors.New("install cancelled")
	}
	idx := 0
	if _, err := fmt.Sscanf(choice, "%d", &idx); err != nil || idx < 1 || idx > len(existing) {
		return integrate.Candidate{}, fmt.Errorf("invalid choice %q", choice)
	}
	return existing[idx-1], nil
}

func pickCreateLocation(cands []integrate.Candidate) (integrate.Candidate, error) {
	fmt.Println("No opencode config found yet. Where should agent-runtime create one?")
	var project, global *integrate.Candidate
	for i := range cands {
		c := &cands[i]
		if c.Scope == integrate.ScopeProject && project == nil {
			project = c
		}
		if c.Scope == integrate.ScopeGlobal && global == nil {
			global = c
		}
	}
	fmt.Printf("  1) %s  (global, available in all projects)\n", global.Path)
	fmt.Printf("  2) %s  (this project only)\n", project.Path)
	fmt.Println("  q) quit")
	choice, err := prompt("\nChoose [1/2]: ")
	if err != nil {
		return integrate.Candidate{}, err
	}
	switch choice {
	case "1":
		return *global, nil
	case "2":
		return *project, nil
	case "q", "Q":
		return integrate.Candidate{}, errors.New("install cancelled")
	}
	return integrate.Candidate{}, fmt.Errorf("invalid choice %q", choice)
}

func confirmCreate(path string) (bool, error) {
	fmt.Printf("No config at %s yet.\n", path)
	return confirm("Create it with agent-runtime configured? [y/N]", false)
}

func showPlan(target integrate.Candidate, exe string) {
	fmt.Printf("\nInstalling into: %s\n", target.Path)
	fmt.Println("Will add to mcp.agent-runtime:")
	fmt.Printf("  %s\n", integrate.OpenCodeEntry([]string{exe, "serve"}))
}

// verifyOpenCode runs `opencode mcp list` and reports whether agent-runtime
// shows up as connected.
func verifyOpenCode(path string) error {
	oc, err := exec.LookPath("opencode")
	if err != nil {
		fmt.Println("note: opencode not on PATH; skipping verification (pass --no-verify to silence)")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, oc, "mcp", "list", "--print-logs")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("verification failed (%v); opencode may need a restart. Run: %s mcp list", err, oc)
	}
	lines := strings.Split(string(out), "\n")
	for _, l := range lines {
		if strings.Contains(l, "agent-runtime") {
			fmt.Println("verification: opencode reports:", strings.TrimSpace(l))
			return nil
		}
	}
	fmt.Printf("verification: config written to %s; opencode shows %d server line(s). If agent-runtime is absent, restart opencode.\n", path, len(lines))
	return nil
}

// isTTY reports whether f is an interactive terminal.
func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// prompt prints a prompt and reads one line from stdin. Interactive only.
func prompt(msg string, args ...any) (string, error) {
	fmt.Printf(msg, args...)
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		fmt.Println()
		return "", errors.New("no input")
	}
	fmt.Println()
	return strings.TrimSpace(sc.Text()), nil
}

// confirm asks a yes/no question with a default.
func confirm(q string, def bool) (bool, error) {
	answer, err := prompt("%s", q)
	if err != nil {
		return false, err
	}
	if answer == "" {
		return def, nil
	}
	switch strings.ToLower(answer) {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	}
	return false, fmt.Errorf("invalid answer %q (y/n)", answer)
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

  opencode install flags:
    --scope global|project         Which config to target (required when ambiguous).
    --yes                          Non-interactive: use the default target, no prompts.
    --create                       Allow creating a config file that does not exist.
    --no-verify                    Skip the post-install ` + "`opencode mcp list`" + ` check.

  agent-runtime version
`)
}
