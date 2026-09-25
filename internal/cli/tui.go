// Bubble Tea setup wizard — the default when `agent-runtime` runs with no
// arguments. Replaces the old line-oriented prompt with a navigable menu:
// arrow keys + enter, space to toggle multi-select, esc to go back, q to
// quit. Every action here is a thin wrapper around the same integrate/*
// package the `integrate` subcommand uses, so nothing here duplicates logic —
// it just makes it discoverable and lets you fan an install out to several
// agents in one go.
package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"agent-runtime/internal/config"
	"agent-runtime/internal/integrate"
	rtmcp "agent-runtime/internal/mcp"
)

// setupMain runs the interactive setup wizard. On non-interactive stdin it
// prints help instead.
func setupMain(loaded *config.Loaded, logger *slog.Logger) error {
	if !isTTY(os.Stdin) {
		root := newRootCmd(nil, loaded, logger)
		return root.Help()
	}
	m := newWizardModel(loaded, logger)
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return err
	}
	if fm, ok := final.(*wizardModel); ok && fm.launchREPL {
		if err := replMain(loaded, logger); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// styling

var (
	colorAccent = lipgloss.AdaptiveColor{Light: "#5B4FE0", Dark: "#9B8CFF"}
	colorMuted  = lipgloss.AdaptiveColor{Light: "#767676", Dark: "#8A8A8A"}
	colorGood   = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#3FB950"}
	colorWarn   = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#D29922"}
	colorBad    = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#F85149"}
	colorBorder = lipgloss.AdaptiveColor{Light: "#D0D0D0", Dark: "#444444"}

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	subtleStyle = lipgloss.NewStyle().Foreground(colorMuted)
	helpStyle   = lipgloss.NewStyle().Foreground(colorMuted).MarginTop(1)
	frameStyle  = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2)
	cursorStyle   = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	selectedStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	itemDescStyle = lipgloss.NewStyle().Foreground(colorMuted)
	goodBadge     = lipgloss.NewStyle().Foreground(colorGood)
	warnBadge     = lipgloss.NewStyle().Foreground(colorWarn)
	badBadge      = lipgloss.NewStyle().Foreground(colorBad)
	checkOn       = lipgloss.NewStyle().Foreground(colorGood).Bold(true)
	errStyle      = lipgloss.NewStyle().Foreground(colorBad)
	okStyle       = lipgloss.NewStyle().Foreground(colorGood)
)

// ---------------------------------------------------------------------------
// agent detection

// agentInfo describes what the wizard knows about one coding agent on this
// machine: whether its CLI binary is on PATH, and whether its config already
// has agent-runtime wired in.
type agentInfo struct {
	agent     integrate.Agent
	label     string
	binary    string
	installed bool // CLI binary found on PATH
	connected bool // config already references agent-runtime
	confPath  string
}

func detectAgents(cwd string, scope integrate.Scope) []agentInfo {
	specs := []struct {
		agent  integrate.Agent
		label  string
		binary string
	}{
		{integrate.Claude, "Claude Code", "claude"},
		{integrate.Codex, "Codex CLI", "codex"},
		{integrate.Gemini, "Gemini CLI", "gemini"},
		{integrate.OpenCode, "opencode", "opencode"},
	}
	out := make([]agentInfo, 0, len(specs))
	for _, s := range specs {
		info := agentInfo{agent: s.agent, label: s.label, binary: s.binary}
		if _, err := exec.LookPath(s.binary); err == nil {
			info.installed = true
		}
		info.confPath, info.connected = agentConnected(s.agent, scope, cwd)
		out = append(out, info)
	}
	return out
}

// agentConnected reports whether an agent's config already contains an
// agent-runtime entry at the given scope, doing a cheap substring check
// rather than a full parse (good enough for a status hint; Integrate does the
// real merge). Codex has no project scope, so it always checks its one global
// file regardless of scope — see integrate.ScopesSupported.
func agentConnected(agent integrate.Agent, scope integrate.Scope, cwd string) (path string, connected bool) {
	if agent == integrate.OpenCode {
		for _, c := range integrate.DiscoverOpenCode(cwd) {
			if c.Scope != scope || !c.Exists {
				continue
			}
			if data, err := os.ReadFile(c.Path); err == nil && strings.Contains(string(data), "agent-runtime") {
				return c.Path, true
			}
		}
		return "", false
	}
	path = integrate.TargetPath(agent, scope, cwd)
	if path == "" {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return path, false
	}
	return path, strings.Contains(string(data), "agent-runtime")
}

// agentSupportsScope reports whether an agent's config format distinguishes
// the given scope (Codex only ever has one global file).
func agentSupportsScope(agent integrate.Agent, scope integrate.Scope) bool {
	for _, s := range integrate.ScopesSupported(agent) {
		if s == scope {
			return true
		}
	}
	return false
}

func statusBadge(info agentInfo) string {
	var badge string
	switch {
	case info.connected:
		badge = goodBadge.Render("● connected")
	case info.installed:
		badge = warnBadge.Render("○ detected, not connected")
	default:
		badge = subtleStyle.Render("· not detected")
	}
	if !agentSupportsScope(info.agent, integrate.ScopeProject) {
		badge += " " + subtleStyle.Render("(global only)")
	}
	return badge
}

// agentSupportsSkills reports whether an agent reads skills from a directory
// this tool can install into (Claude Code and opencode). Codex and Gemini
// have no skill mechanism, so their skill toggle is always off/n-a.
func agentSupportsSkills(agent integrate.Agent) bool {
	return agent == integrate.Claude || agent == integrate.OpenCode
}

// agentSkillDir returns the skill directory a given agent reads a named skill
// from, mirroring the private layout in integrate.skillDirs. Empty for agents
// with no skill mechanism.
func agentSkillDir(agent integrate.Agent, scope integrate.Scope, baseDir, skillName string) string {
	root := baseDir
	if scope != integrate.ScopeProject {
		root, _ = os.UserHomeDir()
	}
	switch agent {
	case integrate.Claude:
		return filepath.Join(root, ".claude", "skills", skillName)
	case integrate.OpenCode:
		if scope == integrate.ScopeProject {
			return filepath.Join(root, ".opencode", "skills", skillName)
		}
		return filepath.Join(root, ".config", "opencode", "skills", skillName)
	}
	return ""
}

// ---------------------------------------------------------------------------
// model

type screen int

const (
	screenHome screen = iota
	screenScaffoldResult
	screenConnectSelect
	screenConnectResult
	screenStatus
	screenRemoveMenu
	screenRemoveSkillsScope
	screenRemoveSkillsResult
	screenRemoveMCPSelect
	screenRemoveMCPResult
	screenHelp
)

type menuItem struct {
	title string
	desc  string
}

type wizardModel struct {
	loaded *config.Loaded
	logger *slog.Logger
	cwd    string

	screen screen
	cursor int

	homeItems []menuItem

	// connect / remove-mcp: multi-select over detected agents. checked marks
	// which agents are included; skillChecked (connect only) marks whether
	// the skill bundle should also be installed for that agent. mcpScope is
	// project or global and applies to the whole batch — tab toggles it and
	// re-detects every agent's connected status at the new scope.
	agents       []agentInfo
	checked      map[int]bool
	skillChecked map[int]bool
	mcpScope     integrate.Scope
	forRemove    bool

	// generic single-select (skills scope, remove menu)
	scopeChoice string

	resultLines []string
	resultErr   error

	launchREPL bool
	quitting   bool
}

func newWizardModel(loaded *config.Loaded, logger *slog.Logger) *wizardModel {
	cwd, _ := os.Getwd()
	m := &wizardModel{
		loaded: loaded,
		logger: logger,
		cwd:    cwd,
		screen: screenHome,
		homeItems: []menuItem{
			{"Init agent-runtime.yaml", "Scaffold a config file for this project"},
			{"Connect AI agents", "Autodetect installed agents; pick one or many to wire up MCP + skills"},
			{"Open process manager", "start/logs/wait/stop — the interactive runtime session"},
			{"Project status", "Config, apps, and what's currently connected"},
			{"Remove", "Uninstall skills or disconnect an agent"},
			{"Help", "Full command reference"},
			{"Exit", ""},
		},
	}
	return m
}

func (m *wizardModel) Init() tea.Cmd { return nil }

// ---------------------------------------------------------------------------
// update

func (m *wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	}

	switch m.screen {
	case screenHome:
		return m.updateHome(keyMsg)
	case screenScaffoldResult, screenConnectResult,
		screenRemoveSkillsResult, screenRemoveMCPResult, screenStatus, screenHelp:
		return m.updateResult(keyMsg)
	case screenRemoveSkillsScope:
		return m.updateRemoveSkillsScope(keyMsg)
	case screenConnectSelect:
		return m.updateMultiSelect(keyMsg, false)
	case screenRemoveMCPSelect:
		return m.updateMultiSelect(keyMsg, true)
	case screenRemoveMenu:
		return m.updateRemoveMenu(keyMsg)
	}
	return m, nil
}

func (m *wizardModel) updateHome(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "up", "k":
		m.cursor = wrap(m.cursor-1, len(m.homeItems))
	case "down", "j":
		m.cursor = wrap(m.cursor+1, len(m.homeItems))
	case "enter":
		switch m.cursor {
		case 0:
			m.runScaffold()
		case 1:
			m.beginConnect(false)
		case 2:
			m.launchREPL = true
			m.quitting = true
			return m, tea.Quit
		case 3:
			m.runStatus()
		case 4:
			m.cursor = 0
			m.screen = screenRemoveMenu
		case 5:
			m.runHelp()
		case 6:
			m.quitting = true
			return m, tea.Quit
		}
	case "q":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (m *wizardModel) updateResult(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q", "esc":
		m.quitting = true
		return m, tea.Quit
	case "enter", " ":
		m.screen = screenHome
		m.cursor = 0
	}
	return m, nil
}

func (m *wizardModel) updateRemoveSkillsScope(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	scopes := []string{"global", "project"}
	switch k.String() {
	case "up", "k", "down", "j":
		i := indexOf(scopes, m.scopeChoice)
		if k.String() == "up" || k.String() == "k" {
			i = wrap(i-1, len(scopes))
		} else {
			i = wrap(i+1, len(scopes))
		}
		m.scopeChoice = scopes[i]
	case "enter":
		m.runRemoveSkills()
	case "esc":
		m.screen = screenHome
		m.cursor = 0
	case "q":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (m *wizardModel) beginConnect(forRemove bool) {
	m.forRemove = forRemove
	m.mcpScope = integrate.ScopeProject
	m.refreshAgents()
	m.cursor = 0
	if forRemove {
		m.screen = screenRemoveMCPSelect
	} else {
		m.screen = screenConnectSelect
	}
}

// refreshAgents re-detects every agent's connected status at m.mcpScope and
// resets the checkboxes to their default for the current mode. Called on
// entry to the connect/remove-mcp screens and whenever the scope is toggled,
// since "connected" means something different at each scope.
func (m *wizardModel) refreshAgents() {
	m.agents = detectAgents(m.cwd, m.mcpScope)
	m.checked = map[int]bool{}
	m.skillChecked = map[int]bool{}
	if !m.forRemove {
		// Pre-check detected-but-not-connected agents — the common case is
		// "wire up whatever I have installed". Skills default on for every
		// agent that can read them; the checkbox only matters once its
		// agent row is checked.
		for i, a := range m.agents {
			if a.installed && !a.connected {
				m.checked[i] = true
			}
			if agentSupportsSkills(a.agent) {
				m.skillChecked[i] = true
			}
		}
	} else {
		for i, a := range m.agents {
			if a.connected {
				m.checked[i] = true
			}
		}
	}
}

func (m *wizardModel) updateMultiSelect(k tea.KeyMsg, forRemove bool) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "up", "k":
		m.cursor = wrap(m.cursor-1, len(m.agents))
	case "down", "j":
		m.cursor = wrap(m.cursor+1, len(m.agents))
	case " ":
		m.checked[m.cursor] = !m.checked[m.cursor]
	case "s":
		// Skill toggle only applies to the connect flow, and only to agents
		// that actually read skills from a directory (Claude Code, opencode).
		if !forRemove && agentSupportsSkills(m.agents[m.cursor].agent) {
			m.skillChecked[m.cursor] = !m.skillChecked[m.cursor]
		}
	case "tab":
		if m.mcpScope == integrate.ScopeProject {
			m.mcpScope = integrate.ScopeGlobal
		} else {
			m.mcpScope = integrate.ScopeProject
		}
		m.refreshAgents()
		m.cursor = 0
	case "a":
		all := true
		for i := range m.agents {
			if !m.checked[i] {
				all = false
				break
			}
		}
		for i := range m.agents {
			m.checked[i] = !all
		}
	case "enter":
		if forRemove {
			m.runRemoveMCP()
		} else {
			m.runConnect()
		}
	case "esc":
		m.screen = screenHome
		m.cursor = 0
	case "q":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (m *wizardModel) updateRemoveMenu(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := 2
	switch k.String() {
	case "up", "k":
		m.cursor = wrap(m.cursor-1, items)
	case "down", "j":
		m.cursor = wrap(m.cursor+1, items)
	case "enter":
		switch m.cursor {
		case 0:
			m.scopeChoice = "global"
			m.cursor = 0
			m.screen = screenRemoveSkillsScope
		case 1:
			m.beginConnect(true)
		}
	case "esc":
		m.screen = screenHome
		m.cursor = 0
	case "q":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// actions — thin wrappers over integrate/*, results captured as lines for the
// result screen.

func (m *wizardModel) runScaffold() {
	m.screen = screenScaffoldResult
	m.resultErr = nil
	if existing := configPathUp(m.cwd); existing != "" {
		m.resultLines = []string{fmt.Sprintf("agent-runtime.yaml already exists at %s (unchanged)", existing)}
		return
	}
	path, created, err := integrate.ScaffoldConfig(m.cwd)
	if err != nil {
		m.resultErr = err
		return
	}
	if created {
		m.resultLines = []string{
			fmt.Sprintf("created %s", path),
			"Add your apps under the apps: block, then use 'start --app <name>' in the process manager.",
		}
	} else {
		m.resultLines = []string{fmt.Sprintf("agent-runtime.yaml already exists at %s (unchanged)", path)}
	}
}

func (m *wizardModel) runRemoveSkills() {
	m.screen = screenRemoveSkillsResult
	m.resultErr = nil
	scope := integrate.Scope(m.scopeChoice)
	var present []string
	for _, d := range integrate.SkillTargetDirs(scope, m.cwd) {
		if _, err := os.Stat(d); err == nil {
			present = append(present, d)
		}
	}
	if len(present) == 0 {
		m.resultLines = []string{fmt.Sprintf("no installed skills to remove (%s scope)", scope)}
		return
	}
	removed, err := integrate.RemoveSkillTrees(scope, m.cwd)
	if err != nil {
		m.resultErr = err
		return
	}
	for _, d := range removed {
		m.resultLines = append(m.resultLines, fmt.Sprintf("removed %s", d))
	}
}

func (m *wizardModel) runConnect() {
	m.screen = screenConnectResult
	m.resultErr = nil
	var any bool
	for i, a := range m.agents {
		if !m.checked[i] {
			continue
		}
		any = true
		m.resultLines = append(m.resultLines, titleStyle.Render(a.label)+":")

		var perErr error
		lines := captureOutput(func() error {
			return Integrate(mcpArgs(a.agent, m.mcpScope, "--write", "--yes"))
		}, &perErr)
		if perErr != nil {
			m.resultLines = append(m.resultLines, errStyle.Render("  "+perErr.Error()))
		} else {
			for _, l := range lines {
				m.resultLines = append(m.resultLines, "  "+l)
			}
		}
		if !agentSupportsScope(a.agent, m.mcpScope) {
			m.resultLines = append(m.resultLines, subtleStyle.Render(fmt.Sprintf("  note: %s has no %s scope; installed globally instead", a.label, m.mcpScope)))
		}

		if agentSupportsSkills(a.agent) && m.skillChecked[i] {
			m.resultLines = append(m.resultLines, m.installSkillsFor(a.agent))
		}
	}
	if !any {
		m.resultLines = []string{"nothing selected — space to toggle, enter to confirm"}
	}
}

// mcpArgs builds the `integrate <agent> ...` argument list, including
// --scope when the agent's config format actually distinguishes it (Codex
// always has exactly one config file, so it's omitted there and Integrate
// falls back to its only supported scope).
func mcpArgs(agent integrate.Agent, scope integrate.Scope, rest ...string) []string {
	args := append([]string{string(agent)}, rest...)
	if agentSupportsScope(agent, scope) {
		args = append(args, "--scope", string(scope))
	}
	return args
}

// installSkillsFor installs the embedded skill bundle into one agent's skill
// directory at m.mcpScope and returns a one-line summary for the result
// screen.
func (m *wizardModel) installSkillsFor(agent integrate.Agent) string {
	var created, updated, unchanged int
	for _, name := range integrate.SkillNames() {
		dir := agentSkillDir(agent, m.mcpScope, m.cwd, name)
		if dir == "" {
			continue
		}
		c, u, un, err := integrate.InstallSkillTree(dir)
		if err != nil {
			return errStyle.Render(fmt.Sprintf("  skills: %v", err))
		}
		created += c
		updated += u
		unchanged += un
	}
	return fmt.Sprintf("  skills installed at %s scope (created=%d updated=%d unchanged=%d)", m.mcpScope, created, updated, unchanged)
}

func (m *wizardModel) runRemoveMCP() {
	m.screen = screenRemoveMCPResult
	m.resultErr = nil
	var any bool
	for i, a := range m.agents {
		if !m.checked[i] {
			continue
		}
		any = true
		var perErr error
		lines := captureOutput(func() error {
			return Integrate(mcpArgs(a.agent, m.mcpScope, "--remove", "--yes"))
		}, &perErr)
		m.resultLines = append(m.resultLines, titleStyle.Render(a.label)+":")
		if perErr != nil {
			m.resultLines = append(m.resultLines, errStyle.Render("  "+perErr.Error()))
		} else {
			for _, l := range lines {
				m.resultLines = append(m.resultLines, "  "+l)
			}
		}
	}
	if !any {
		m.resultLines = []string{"nothing selected — space to toggle, enter to confirm"}
	}
}

func (m *wizardModel) runStatus() {
	m.screen = screenStatus
	m.resultErr = nil
	var lines []string
	lines = append(lines, fmt.Sprintf("project dir: %s", m.cwd))
	if m.loaded.Path != "" {
		lines = append(lines, fmt.Sprintf("config: %s", m.loaded.Path))
		if len(m.loaded.Config.Apps) > 0 {
			lines = append(lines, "apps:")
			for _, n := range m.loaded.Names() {
				lines = append(lines, "  "+n)
			}
		} else {
			lines = append(lines, "apps: (none configured yet)")
		}
	} else {
		lines = append(lines, "config: none — run 'Init agent-runtime.yaml' from the home menu")
	}
	lines = append(lines, "")
	lines = append(lines, titleStyle.Render("coding agents"))
	project := detectAgents(m.cwd, integrate.ScopeProject)
	global := detectAgents(m.cwd, integrate.ScopeGlobal)
	for i := range project {
		p, g := project[i], global[i]
		if !agentSupportsScope(p.agent, integrate.ScopeProject) {
			lines = append(lines, fmt.Sprintf("  %-14s %s", p.label, statusBadge(g)))
			continue
		}
		lines = append(lines, fmt.Sprintf("  %-14s project: %s  global: %s", p.label, statusBadge(p), statusBadge(g)))
	}
	lines = append(lines, "")
	lines = append(lines, titleStyle.Render("skills"))
	home, _ := os.UserHomeDir()
	lines = append(lines, skillLine("Claude Code (global)", filepath.Join(home, ".claude", "skills")))
	lines = append(lines, skillLine("opencode (global)", filepath.Join(home, ".config", "opencode", "skills")))
	lines = append(lines, skillLine("Claude Code (project)", filepath.Join(m.cwd, ".claude", "skills")))
	lines = append(lines, skillLine("opencode (project)", filepath.Join(m.cwd, ".opencode", "skills")))
	m.resultLines = lines
}

func skillLine(label, root string) string {
	var installed []string
	for _, name := range integrate.SkillNames() {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			installed = append(installed, name)
		}
	}
	if len(installed) > 0 {
		return fmt.Sprintf("  %-22s %s", label, goodBadge.Render(strings.Join(installed, ", ")))
	}
	return fmt.Sprintf("  %-22s %s", label, subtleStyle.Render("none"))
}

func (m *wizardModel) runHelp() {
	m.screen = screenHelp
	m.resultErr = nil
	m.resultLines = []string{
		"serve                Run the MCP stdio server (what coding agents launch).",
		"repl                 Open the interactive process manager (start/logs/wait/stop).",
		"run <cmd> [args...]  Start a process in the foreground and tail its output.",
		"shell <app|proc>     Start an interactive shell in a process's environment.",
		"integrate <agent>    Print, install (--write) or remove (--remove) MCP config.",
		"integrate skill      Install (--write) or remove (--remove) embedded skills.",
		"",
		"Run 'agent-runtime <command> --help' for details on any of these.",
	}
}

// captureOutput runs fn with os.Stdout redirected to a pipe and returns the
// printed lines, so the existing print-oriented Integrate/etc. functions can
// feed the result screen without changing their signatures.
func captureOutput(fn func() error, errOut *error) []string {
	r, w, err := os.Pipe()
	if err != nil {
		*errOut = fn()
		return nil
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan []string, 1)
	go func() {
		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 4096)
		for {
			n, rerr := r.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if rerr != nil {
				break
			}
		}
		done <- strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	}()
	*errOut = fn()
	os.Stdout = old
	w.Close()
	lines := <-done
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

// ---------------------------------------------------------------------------
// view

func (m *wizardModel) View() string {
	if m.quitting {
		if m.launchREPL {
			return ""
		}
		return "bye\n"
	}
	var body string
	switch m.screen {
	case screenHome:
		body = m.viewHome()
	case screenRemoveSkillsScope:
		body = m.viewScopeSelect("Remove skills", "choose the scope to uninstall from")
	case screenConnectSelect:
		body = m.viewMultiSelect("Connect AI agents", "space toggles the agent (installs MCP) · s toggles its skill bundle")
	case screenRemoveMCPSelect:
		body = m.viewMultiSelect("Disconnect AI agents", "space to toggle, a to toggle all, enter to remove")
	case screenRemoveMenu:
		body = m.viewRemoveMenu()
	case screenScaffoldResult, screenConnectResult,
		screenRemoveSkillsResult, screenRemoveMCPResult, screenStatus, screenHelp:
		body = m.viewResult()
	}
	return frameStyle.Render(body)
}

func (m *wizardModel) header(sub string) string {
	title := titleStyle.Render(fmt.Sprintf("agent-runtime %s", rtmcp.Version))
	if sub == "" {
		return title + "\n" + subtleStyle.Render("project setup & management") + "\n\n"
	}
	return title + "  " + subtleStyle.Render(sub) + "\n\n"
}

func (m *wizardModel) viewHome() string {
	var b strings.Builder
	b.WriteString(m.header(""))
	for i, item := range m.homeItems {
		b.WriteString(renderMenuLine(i == m.cursor, item.title, item.desc))
		b.WriteString("\n")
	}
	b.WriteString(helpStyle.Render("↑/↓ move · enter select · q quit"))
	return b.String()
}

func (m *wizardModel) viewRemoveMenu() string {
	items := []menuItem{
		{"Skills", "Uninstall the embedded skills (choose global or project scope)"},
		{"MCP config", "Disconnect one or more agents"},
	}
	var b strings.Builder
	b.WriteString(m.header("remove"))
	for i, item := range items {
		b.WriteString(renderMenuLine(i == m.cursor, item.title, item.desc))
		b.WriteString("\n")
	}
	b.WriteString(helpStyle.Render("↑/↓ move · enter select · esc back · q quit"))
	return b.String()
}

func (m *wizardModel) viewScopeSelect(title, sub string) string {
	var b strings.Builder
	b.WriteString(m.header(title))
	b.WriteString(subtleStyle.Render(sub) + "\n\n")
	scopes := []struct{ key, desc string }{
		{"global", "applies to every project on this machine"},
		{"project", "applies only to " + m.cwd},
	}
	for _, s := range scopes {
		b.WriteString(renderMenuLine(m.scopeChoice == s.key, s.key, s.desc))
		b.WriteString("\n")
	}
	b.WriteString(helpStyle.Render("↑/↓ choose · enter confirm · esc back · q quit"))
	return b.String()
}

func (m *wizardModel) viewMultiSelect(title, sub string) string {
	var b strings.Builder
	b.WriteString(m.header(title))
	b.WriteString(subtleStyle.Render(sub) + "\n")
	b.WriteString(m.scopeLine() + "\n\n")
	for i, a := range m.agents {
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("> ")
		}
		box := "[ ]"
		if m.checked[i] {
			box = checkOn.Render("[x]")
		}
		name := a.label
		if i == m.cursor {
			name = selectedStyle.Render(name)
		}
		line := fmt.Sprintf("%s%s %-14s %s", cursor, box, name, statusBadge(a))
		if !m.forRemove {
			line += "  " + m.skillCell(i, a)
		}
		b.WriteString(line + "\n")
	}
	if m.forRemove {
		b.WriteString(helpStyle.Render("↑/↓ move · space toggle · tab switch scope · a toggle all · enter confirm · esc back · q quit"))
	} else {
		b.WriteString(helpStyle.Render("↑/↓ move · space toggle agent (mcp) · s toggle skills · tab switch scope · a toggle all · enter install · esc back · q quit"))
	}
	return b.String()
}

// scopeLine renders the current project/global scope for the connect and
// remove-mcp screens, with the inactive one dimmed so tab's effect is obvious.
func (m *wizardModel) scopeLine() string {
	project, global := "project", "global"
	if m.mcpScope == integrate.ScopeProject {
		project = selectedStyle.Render("[project]")
		global = subtleStyle.Render(" global ")
	} else {
		project = subtleStyle.Render(" project ")
		global = selectedStyle.Render("[global]")
	}
	return fmt.Sprintf("scope: %s  %s  %s", project, global, subtleStyle.Render(fmt.Sprintf("— %s scope installs into %s", m.mcpScope, scopeHint(m.mcpScope, m.cwd))))
}

func scopeHint(scope integrate.Scope, cwd string) string {
	if scope == integrate.ScopeProject {
		return cwd
	}
	return "your home directory (every project)"
}

// skillCell renders the skill-bundle toggle for one agent row on the connect
// screen. MCP is implied by the row's own checkbox (always included once an
// agent is checked); skills are an independent, toggleable add-on shown only
// for agents that actually read a skill directory.
func (m *wizardModel) skillCell(i int, a agentInfo) string {
	if !agentSupportsSkills(a.agent) {
		return subtleStyle.Render("skills: n/a")
	}
	if !m.checked[i] {
		return subtleStyle.Render("skills: —")
	}
	if m.skillChecked[i] {
		return "skills:" + checkOn.Render("[x]")
	}
	return "skills:[ ]"
}

func (m *wizardModel) viewResult() string {
	var b strings.Builder
	b.WriteString(m.header(""))
	if m.resultErr != nil {
		b.WriteString(errStyle.Render("error: "+m.resultErr.Error()) + "\n")
	}
	for _, l := range m.resultLines {
		b.WriteString(l + "\n")
	}
	b.WriteString(helpStyle.Render("\nenter/space back to menu · q quit"))
	return b.String()
}

func renderMenuLine(active bool, title, desc string) string {
	cursor := "  "
	t := title
	if active {
		cursor = cursorStyle.Render("> ")
		t = selectedStyle.Render(title)
	}
	if desc == "" {
		return cursor + t
	}
	return fmt.Sprintf("%s%-24s %s", cursor, t, itemDescStyle.Render(desc))
}

// ---------------------------------------------------------------------------
// small helpers shared with the old setup.go plumbing

func wrap(i, n int) int {
	if n == 0 {
		return 0
	}
	i %= n
	if i < 0 {
		i += n
	}
	return i
}

func indexOf(ss []string, s string) int {
	for i, v := range ss {
		if v == s {
			return i
		}
	}
	return 0
}

var _ = okStyle // reserved for future use (kept to avoid unused-var churn)
