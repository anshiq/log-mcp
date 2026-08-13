// Interactive setup wizard — the default when `agent-runtime` runs with no
// arguments. From one menu you can scaffold agent-runtime.yaml, install the
// embedded skills, wire up an AI coding agent, or jump into the process
// manager. The process manager itself lives under `agent-runtime repl`.
package cli

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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
	reader := bufio.NewReader(os.Stdin)
	for {
		setupMenu()
		choice, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println()
			return nil
		}
		switch strings.TrimSpace(choice) {
		case "1":
			setupScaffold()
		case "2":
			setupInstallSkills(reader)
		case "3":
			setupIntegrate(reader)
		case "4":
			if err := replMain(loaded, logger); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			}
			fmt.Println("\nback at the setup menu")
		case "5":
			setupStatus(loaded)
		case "6":
			setupRemove(reader)
		case "7":
			root := newRootCmd(nil, loaded, logger)
			_ = root.Help()
		case "8", "q", "Q", "quit", "exit":
			fmt.Println("bye")
			return nil
		default:
			fmt.Printf("invalid choice %q\n", choice)
		}
	}
}

func setupMenu() {
	fmt.Printf("\nagent-runtime %s — project setup & management\n\n", rtmcp.Version)
	fmt.Println("What would you like to do?")
	fmt.Println("  1) Init agent-runtime.yaml in this project")
	fmt.Println("  2) Install agent-runtime skills (agent-runtime-ready, agent-runtime-logging)")
	fmt.Println("  3) Connect an AI coding agent (claude|codex|gemini|opencode)")
	fmt.Println("  4) Open the interactive process manager (start/logs/wait/stop)")
	fmt.Println("  5) Show project status")
	fmt.Println("  6) Remove agent-runtime skills or MCP config")
	fmt.Println("  7) Print help")
	fmt.Println("  8) Exit")
	fmt.Print("\nchoose> ")
}

// setupScaffold creates agent-runtime.yaml in the current project unless one
// already exists (here or in a parent directory).
func setupScaffold() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	if existing := configPathUp(cwd); existing != "" {
		fmt.Printf("agent-runtime.yaml already exists at %s (unchanged)\n", existing)
		return
	}
	path, created, err := integrate.ScaffoldConfig(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	if created {
		fmt.Printf("created %s\n", path)
		fmt.Println("add your apps under the apps: block, then use 'start --app <name>' in the process manager.")
	} else {
		fmt.Printf("agent-runtime.yaml already exists at %s (unchanged)\n", path)
	}
}

// setupInstallSkills installs the embedded skills at global or project scope.
func setupInstallSkills(reader *bufio.Reader) {
	fmt.Print("install at [g]lobal (all projects) or [p]roject (this project)? [g/p] ")
	scope := "global"
	if line, err := reader.ReadString('\n'); err == nil {
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "p", "project":
			scope = "project"
		}
	}
	args := []string{"skill", "--write"}
	if scope == "project" {
		args = append(args, "--scope", "project")
	}
	if err := Integrate(args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}
}

// setupIntegrate prints and optionally installs the MCP config block for one
// coding agent.
func setupIntegrate(reader *bufio.Reader) {
	fmt.Print("which agent? [claude|codex|gemini|opencode] ")
	line, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	agent := strings.ToLower(strings.TrimSpace(line))
	switch agent {
	case "claude", "codex", "gemini", "opencode":
	default:
		fmt.Printf("unknown agent %q\n", agent)
		return
	}
	if err := Integrate([]string{agent}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	fmt.Print("\nwrite this config to the agent's file? [y/N] ")
	if line, err := reader.ReadString('\n'); err == nil && strings.EqualFold(strings.TrimSpace(line), "y") {
		if err := Integrate([]string{agent, "--write"}); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
	}
}

// setupRemove opens the removal submenu: uninstall the embedded skills or strip
// the agent-runtime MCP entry out of an agent's config. The actual removals go
// through Integrate, which confirms before touching anything interactively.
func setupRemove(reader *bufio.Reader) {
	for {
		fmt.Println("\nWhat would you like to remove?")
		fmt.Println("  1) agent-runtime skills (installed files)")
		fmt.Println("  2) MCP config for an agent (claude|codex|gemini|opencode)")
		fmt.Println("  q) back to main menu")
		fmt.Print("\nchoose> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println()
			return
		}
		switch strings.TrimSpace(line) {
		case "1":
			setupRemoveSkills(reader)
		case "2":
			setupRemoveMCP(reader)
		case "q", "Q", "back":
			return
		default:
			fmt.Printf("invalid choice %q\n", line)
		}
	}
}

// setupRemoveSkills uninstalls the embedded skills at global or project scope.
func setupRemoveSkills(reader *bufio.Reader) {
	fmt.Print("remove at [g]lobal (all projects) or [p]roject (this project)? [g/p] ")
	scope := "global"
	if line, err := reader.ReadString('\n'); err == nil {
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "p", "project":
			scope = "project"
		}
	}
	args := []string{"skill", "--remove"}
	if scope == "project" {
		args = append(args, "--scope", "project")
	}
	if err := Integrate(args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}
}

// setupRemoveMCP strips the agent-runtime MCP entry from one agent's config.
func setupRemoveMCP(reader *bufio.Reader) {
	fmt.Print("which agent? [claude|codex|gemini|opencode] ")
	line, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	agent := strings.ToLower(strings.TrimSpace(line))
	switch agent {
	case "claude", "codex", "gemini", "opencode":
	default:
		fmt.Printf("unknown agent %q\n", agent)
		return
	}
	if err := Integrate([]string{agent, "--remove"}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}
}

// setupStatus reports the project's config and which skills are installed.
func setupStatus(loaded *config.Loaded) {
	cwd, _ := os.Getwd()
	fmt.Printf("\nproject dir: %s\n", cwd)
	if loaded.Path != "" {
		fmt.Printf("config: %s\n", loaded.Path)
		if len(loaded.Config.Apps) > 0 {
			fmt.Println("apps:")
			for _, n := range loaded.Names() {
				fmt.Printf("  %s\n", n)
			}
		} else {
			fmt.Println("apps: (none configured yet)")
		}
	} else {
		fmt.Println("config: none — choose '1) Init agent-runtime.yaml' to create one")
	}
	home, _ := os.UserHomeDir()
	reportSkillTree("Claude Code (global)", filepath.Join(home, ".claude", "skills"))
	reportSkillTree("opencode (global)", filepath.Join(home, ".config", "opencode", "skills"))
	reportSkillTree("Claude Code (project)", filepath.Join(cwd, ".claude", "skills"))
	reportSkillTree("opencode (project)", filepath.Join(cwd, ".opencode", "skills"))
}

func reportSkillTree(label, root string) {
	var installed []string
	for _, name := range integrate.SkillNames() {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			installed = append(installed, name)
		}
	}
	if len(installed) > 0 {
		fmt.Printf("skills installed for %s: %s\n", label, strings.Join(installed, ", "))
	} else {
		fmt.Printf("skills installed for %s: none\n", label)
	}
}

// configPathUp walks from dir upward looking for agent-runtime.yaml/.yml,
// mirroring config.LoadDefault's resolution for the scaffold check.
func configPathUp(dir string) string {
	for {
		for _, name := range []string{"agent-runtime.yaml", "agent-runtime.yml"} {
			p := filepath.Join(dir, name)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
