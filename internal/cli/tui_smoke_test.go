package cli

import (
	"strings"
	"testing"

	"agent-runtime/internal/config"
	"agent-runtime/internal/integrate"
	tea "github.com/charmbracelet/bubbletea"
)

func TestWizardSmoke(t *testing.T) {
	m := newWizardModel(&config.Loaded{}, nil)
	view := m.View()
	if !strings.Contains(view, "agent-runtime") || !strings.Contains(view, "Init agent-runtime.yaml") {
		t.Fatalf("home view missing content:\n%s", view)
	}
	// navigate to Connect AI agents
	m.cursor = 1
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(*wizardModel)
	if m.screen != screenConnectSelect {
		t.Fatalf("expected connect select screen, got %v", m.screen)
	}
	view = m.View()
	if !strings.Contains(view, "Claude Code") || !strings.Contains(view, "skills") {
		t.Fatalf("connect view missing agents/skills column:\n%s", view)
	}

	// checking an agent should default its skill toggle on (Claude supports skills).
	m.cursor = 0
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mm.(*wizardModel)
	if m.checked[0] && !m.skillChecked[0] {
		t.Fatalf("expected skill toggle pre-checked once agent is checked")
	}
	// 's' toggles the skill bundle independently of the agent checkbox.
	before := m.skillChecked[0]
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = mm.(*wizardModel)
	if m.skillChecked[0] == before {
		t.Fatalf("expected 's' to toggle skillChecked[0]")
	}

	// tab flips project/global scope and re-detects agents, resetting the
	// per-agent checkboxes since "connected" means something different at
	// each scope.
	if m.mcpScope != integrate.ScopeProject {
		t.Fatalf("expected default scope project, got %v", m.mcpScope)
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = mm.(*wizardModel)
	if m.mcpScope != integrate.ScopeGlobal {
		t.Fatalf("expected tab to switch scope to global, got %v", m.mcpScope)
	}
	view = m.View()
	if !strings.Contains(view, "[global]") {
		t.Fatalf("connect view missing active scope marker:\n%s", view)
	}

	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mm.(*wizardModel)
	if m.screen != screenHome {
		t.Fatalf("expected home after esc, got %v", m.screen)
	}

	// status screen
	m.cursor = 3
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(*wizardModel)
	view = m.View()
	if !strings.Contains(view, "coding agents") {
		t.Fatalf("status view missing section:\n%s", view)
	}
}
