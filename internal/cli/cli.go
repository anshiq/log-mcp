// Package cli implements the agent-runtime command-line interface. Running
// with no arguments opens the native GUI window (desktop build with a display)
// or the command help; the MCP server runs under "serve" and the process
// manager under "repl" (repl.go). run/shell/integrate/version are one-shot
// helpers, and the CLI wiring lives in root.go.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"agent-runtime/internal/config"
	"agent-runtime/internal/daemon"
	"agent-runtime/internal/httpserve"
	"agent-runtime/internal/integrate"
	rtmcp "agent-runtime/internal/mcp"
	"agent-runtime/internal/platform/paths"
	"agent-runtime/internal/runtime"
)

// Serve runs the MCP stdio server until the client disconnects or a shutdown
// signal arrives. In v3 the bridge dials the per-user daemon (started on
// demand) and all sessions share its processes; --embedded keeps the legacy
// session-scoped mode for one minor release to de-risk the rollout.
func Serve(loaded *config.Loaded, logger *slog.Logger) error {
	return serveStdio(loaded, logger, serveOptions{})
}

type serveOptions struct {
	embedded bool
	project  string
}

// serveStdio runs the MCP bridge (default) or the embedded runtime.
func serveStdio(loaded *config.Loaded, logger *slog.Logger, opts serveOptions) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !opts.embedded {
		if bridge, err := dialBridge(loaded, logger, opts); err == nil {
			defer bridge.Close()
			server := rtmcp.NewServer(bridge, logger)
			return server.Run(ctx, &mcp.StdioTransport{})
		} else {
			logger.Warn("daemon unreachable; falling back to embedded runtime", "error", err)
		}
	}

	rt := runtime.New(loaded, logger)
	server := rtmcp.NewServer(rt, logger)
	err := server.Run(ctx, &mcp.StdioTransport{})
	if serr := rt.Shutdown(); err == nil {
		err = serr
	}
	return err
}

// dialBridge connects serve to the per-user daemon: socket discovery,
// version handshake, workspace resolution and session registration.
func dialBridge(loaded *config.Loaded, logger *slog.Logger, opts serveOptions) (*rtmcp.Bridge, error) {
	p := paths.User()
	if err := p.EnsureDirs(); err != nil {
		return nil, err
	}
	// On-demand spawn: the first agent starts agentd via the flock race.
	d := daemon.New(p.SocketPath(), p.Data)
	if !d.IsRunning() {
		if err := d.Start(); err != nil {
			return nil, fmt.Errorf("start daemon: %w", err)
		}
		if err := d.WaitReady(5 * time.Second); err != nil {
			return nil, err
		}
	}
	workspace := opts.project
	if workspace == "" {
		workspace = loaded.ProjectDir
	}
	return rtmcp.DialBridge(p.SocketPath(), workspace, "", logger)
}

// ServeHTTP runs the MCP server over the streamable-HTTP transport on addr
// (Phase 5). A bearer token from runtime.http.token (env-expanded) is required;
// requests without it get 401 and tool calls are rate-limited. The process
// registry is shared (daemon semantics when runtime.daemon is set); each HTTP
// request session is stateless at the transport.
func ServeHTTP(loaded *config.Loaded, logger *slog.Logger, addr string) error {
	if addr == "" {
		addr = loaded.Config.Runtime.HTTP.Addr
	}
	if addr == "" {
		return errors.New("serve --http requires a listen address (or runtime.http.addr)")
	}
	token := os.ExpandEnv(loaded.Config.Runtime.HTTP.Token)
	if token == "" {
		return errors.New("serve --http requires runtime.http.token; there is no unauthenticated HTTP mode")
	}

	var facade runtime.Facade
	var shutdown func() error
	var bridge *rtmcp.Bridge
	if loaded.Config.Runtime.Daemon {
		// Legacy per-project daemon path (v2 compat).
		started, err := daemon.Start(loaded, logger)
		if err != nil {
			return err
		}
		if started {
			if err := daemon.WaitReady(loaded.ProjectDir, 10*time.Second); err != nil {
				return err
			}
		}
		facade = daemon.NewClient(daemon.SocketPath(loaded.ProjectDir))
		shutdown = func() error { return nil } // daemon owns the processes
	} else if b, err := dialBridge(loaded, logger, serveOptions{}); err == nil {
		// v3 bridge: remote MCP clients share the per-user daemon.
		bridge = b
		facade = b
		shutdown = func() error { return b.Close() }
	} else {
		logger.Warn("daemon unreachable for --http; serving embedded", "error", err)
		rt := runtime.New(loaded, logger)
		facade = rt
		shutdown = rt.Shutdown
	}
	_ = bridge

	server := rtmcp.NewServer(facade, logger)
	handler := httpserve.Handler(server, token, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{Addr: addr, Handler: handler}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()

	logger.Info("serving streamable-HTTP", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return shutdown()
}

// Integrate prints (or, with --write, installs; with --remove, uninstalls) the
// MCP client configuration for the requested agent.
func Integrate(args []string) error {
	opts, pos, err := parseIntegrateArgs(args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: agent-runtime integrate <claude|codex|gemini|opencode|generic|skill|status> [--write] [--remove] [--scope global|project] [--yes] [--create] [--no-verify]")
	}
	if pos[0] == "status" {
		return integrateStatus()
	}
	if pos[0] == "skill" {
		return integrateSkill(opts)
	}
	agent := integrate.Agent(pos[0])
	if opts.remove {
		return integrateRemove(agent, opts)
	}
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
	scope, err := scopeFor(agent, opts)
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	path, err := integrate.Write(agent, scope, exe, cwd)
	if err != nil {
		return err
	}
	fmt.Printf("wrote agent-runtime config to %s (%s scope)\n", path, scope)
	return nil
}

// scopeFor resolves the --scope flag against what an agent's config format
// actually supports, falling back to its historical default when --scope is
// omitted: project for Claude (its shared, checked-in .mcp.json), global for
// everyone else (Codex has no project scope at all).
func scopeFor(agent integrate.Agent, opts integrateOptions) (integrate.Scope, error) {
	if opts.scope == "" {
		if agent == integrate.Claude {
			return integrate.ScopeProject, nil
		}
		return integrate.ScopeGlobal, nil
	}
	if err := validateScope(opts.scope); err != nil {
		return "", err
	}
	scope := integrate.Scope(opts.scope)
	supported := integrate.ScopesSupported(agent)
	for _, s := range supported {
		if s == scope {
			return scope, nil
		}
	}
	return "", fmt.Errorf("%s only supports %s scope", agent, scopeNames(supported))
}

func scopeNames(scopes []integrate.Scope) string {
	names := make([]string, len(scopes))
	for i, s := range scopes {
		names[i] = string(s)
	}
	return strings.Join(names, "/")
}

// integrateSkill prints the install plan for, installs (--write), or removes
// (--remove) the embedded skill bundle. Skills install at global scope only.
func integrateSkill(opts integrateOptions) error {
	if opts.scope != "" {
		if err := validateScope(opts.scope); err != nil {
			return err
		}
		if opts.scope != string(integrate.ScopeGlobal) {
			return errors.New("skills only support global scope")
		}
	}
	if opts.remove {
		var present []string
		for _, d := range integrate.SkillTargetDirs() {
			if _, err := os.Stat(d); err == nil {
				present = append(present, d)
			}
		}
		if len(present) == 0 {
			fmt.Println("no installed skills to remove")
			return nil
		}
		fmt.Println("skills installed:")
		for _, d := range present {
			fmt.Printf("  %s\n", d)
		}
		if isTTY(os.Stdin) {
			ok, err := confirm("Remove these skill directories? [y/N]", false)
			if err != nil {
				return err
			}
			if !ok {
				return errors.New("removal cancelled")
			}
		}
		removed, err := integrate.RemoveSkillTrees()
		if err != nil {
			return err
		}
		for _, d := range removed {
			fmt.Printf("removed %s\n", d)
		}
		return nil
	}
	dirs := integrate.SkillTargetDirs()
	if !opts.write {
		fmt.Printf("embedded skills: %s\n", strings.Join(integrate.SkillNames(), ", "))
		fmt.Println("skill target dirs:")
		for _, d := range dirs {
			fmt.Printf("  %s\n", d)
		}
		fmt.Println("Files:")
		for _, f := range integrate.SkillFiles() {
			fmt.Printf("  %s\n", f)
		}
		fmt.Println("\n# To install: agent-runtime integrate skill --write")
		return nil
	}
	for _, d := range dirs {
		created, updated, unchanged, err := integrate.InstallSkillTree(d)
		if err != nil {
			return fmt.Errorf("%s: %w", d, err)
		}
		fmt.Printf("installed %s (created=%d updated=%d unchanged=%d)\n", d, created, updated, unchanged)
	}
	return nil
}

// integrateStatus prints the harness matrix: detected binaries, MCP and
// skills state per harness (global scope). GUI-independent exit for Phase 6.
func integrateStatus() error {
	fmt.Printf("%-15s %-9s %-8s %-7s %s\n", "HARNESS", "DETECTED", "MCP", "SKILLS", "VERSION")
	for _, d := range integrate.Descriptors() {
		det := integrate.DetectGlobal(d)
		mcp := "-"
		if det.MCPConfigured {
			mcp = "yes"
		}
		skills := "-"
		if len(det.SkillsInstalled) > 0 {
			skills = strings.Join(det.SkillsInstalled, ",")
		}
		detected := "-"
		if det.Detected {
			detected = "yes"
		}
		fmt.Printf("%-15s %-9s %-8s %-7s %s\n", d.ID, detected, mcp, skills, det.Version)
	}
	if updates := integrate.CheckSkillUpdates(); len(updates) > 0 {
		fmt.Println("\noutdated skills:")
		for _, u := range updates {
			fmt.Printf("  %s/%s: have %q want %q\n", u.Harness, u.Name, u.Have, u.Want)
		}
	}
	return nil
}

// integrateOptions holds the parsed flags for `integrate`.
type integrateOptions struct {
	write        bool
	remove       bool
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
		case a == "--remove" || a == "-remove":
			opts.remove = true
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
		target, err = pickExisting(existing, "install")
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

// integrateRemove removes the agent-runtime MCP entry from an agent's config,
// confirming first when run interactively.
func integrateRemove(agent integrate.Agent, opts integrateOptions) error {
	if agent == integrate.OpenCode {
		return removeOpenCode(opts)
	}
	if agent == integrate.Generic {
		return errors.New("--remove is not supported for the generic agent")
	}
	scope, err := scopeFor(agent, opts)
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	path := integrate.TargetPath(agent, scope, cwd)
	if path == "" {
		return fmt.Errorf("unknown agent %q", agent)
	}
	if isTTY(os.Stdin) && !opts.yes {
		ok, err := confirm(fmt.Sprintf("Remove agent-runtime from %s? [y/N]", path), false)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("removal cancelled")
		}
	}
	_, changed, err := integrate.Remove(agent, scope, cwd)
	if err != nil {
		return err
	}
	if !changed {
		fmt.Printf("agent-runtime is not configured in %s\n", path)
		return nil
	}
	fmt.Printf("removed agent-runtime from %s\n", path)
	return nil
}

// removeOpenCode removes the agent-runtime entry from an existing opencode
// config. Like integrateOpenCode it discovers the real config locations and
// lets the user pick when several exist.
func removeOpenCode(opts integrateOptions) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cands := integrate.DiscoverOpenCode(cwd)
	existing := existingCandidates(cands)
	if opts.scope != "" {
		if err := validateScope(opts.scope); err != nil {
			return err
		}
		existing = existingCandidates(filterScope(cands, integrate.Scope(opts.scope)))
	}
	interactive := isTTY(os.Stdin) && !opts.yes
	var target integrate.Candidate
	switch {
	case len(existing) == 0 && !interactive:
		return fmt.Errorf("no opencode config found with agent-runtime configured")
	case len(existing) == 0:
		fmt.Println("no opencode config found with agent-runtime configured")
		return nil
	case len(existing) > 1 && !interactive:
		return fmt.Errorf("multiple opencode configs found; pick one with --scope %s", scopesOf(existing))
	case len(existing) > 1:
		target, err = pickExisting(existing, "remove")
		if err != nil {
			return err
		}
	default:
		target = existing[0]
	}
	if interactive {
		ok, err := confirm(fmt.Sprintf("Remove agent-runtime from %s? [y/N]", target.Path), false)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("removal cancelled")
		}
	}
	changed, err := integrate.RemoveOpenCodeConfig(target.Path)
	if err != nil {
		return err
	}
	if !changed {
		fmt.Printf("agent-runtime is not configured in %s\n", target.Path)
		return nil
	}
	fmt.Printf("removed agent-runtime from %s\n", target.Path)
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

func pickExisting(existing []integrate.Candidate, action string) (integrate.Candidate, error) {
	fmt.Println("Existing opencode config(s):")
	for i, c := range existing {
		fmt.Printf("  %d) %s  (%s)\n", i+1, c.Path, c.Scope)
	}
	fmt.Println("  q) quit")
	choice, err := prompt("\nChoose where to %s [1-%d]: ", action, len(existing))
	if err != nil {
		return integrate.Candidate{}, err
	}
	if choice == "q" || choice == "Q" {
		return integrate.Candidate{}, fmt.Errorf("%s cancelled", action)
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
