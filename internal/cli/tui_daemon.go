// Full TUI on pkg/client (Phase 7/9): Bubble Tea dashboard mirroring the
// GUI's Processes / Logs / Integrations screens. The setup wizard in
// tui.go stays the no-args entry; `tui` needs a running daemon.
package cli

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"agent-runtime/internal/config"
	"agent-runtime/pkg/client"
)

func newTuiCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Open the terminal dashboard (daemon)",
		Long:  "Open the terminal dashboard over the per-user daemon (started on demand). The web UI (`agent-runtime web`) is the primary interface.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := ensureDaemonClient("")
			if err != nil {
				return err
			}
			_, ws, err := resolveWorkspace(cl, project)
			if err != nil {
				return err
			}
			m := newTuiModel(cl, ws)
			p := tea.NewProgram(m, tea.WithAltScreen())
			_, err = p.Run()
			return err
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "workspace path or id")
	return cmd
}

type tuiModel struct {
	cl        *client.Client
	workspace string
	procs     []map[string]any
	cursor    int
	logs      []string
	mode      string // processes | logs
	status    string
	width     int
	height    int
}

type tuiTick struct{}

func newTuiModel(cl *client.Client, ws string) tuiModel {
	return tuiModel{cl: cl, workspace: ws, mode: "processes"}
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(tuiRefresh(), tea.EnterAltScreen)
}

func tuiRefresh() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return tuiTick{} })
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tuiTick:
		m.refresh()
		return m, tuiRefresh()
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.procs)-1 {
				m.cursor++
			}
		case "enter", "l":
			if m.mode == "processes" && len(m.procs) > 0 {
				m.mode = "logs"
				m.tailSelected()
			}
		case "esc", "h":
			m.mode = "processes"
		case "r":
			m.act("restart")
		case "s":
			m.act("stop")
		}
	}
	return m, nil
}

func (m *tuiModel) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	items, err := m.cl.ProcessService().List(ctx, m.workspace, false)
	if err != nil {
		m.status = "error: " + err.Error()
		return
	}
	m.procs = items
	if m.cursor >= len(items) && len(items) > 0 {
		m.cursor = len(items) - 1
	}
	m.status = fmt.Sprintf("%d processes", len(items))
}

func (m *tuiModel) selectedID() string {
	if len(m.procs) == 0 {
		return ""
	}
	id, _ := m.procs[m.cursor]["process_id"].(string)
	if id == "" {
		id, _ = m.procs[m.cursor]["id"].(string)
	}
	return id
}

func (m *tuiModel) tailSelected() {
	id := m.selectedID()
	if id == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	res, err := m.cl.LogService().GetLogs(ctx, id, 100)
	if err != nil {
		m.status = "logs: " + err.Error()
		return
	}
	m.logs = nil
	if entries, ok := res["entries"].([]any); ok {
		for _, e := range entries {
			if em, ok := e.(map[string]any); ok {
				m.logs = append(m.logs, fmt.Sprint(em["line"]))
			}
		}
	}
}

func (m *tuiModel) act(action string) {
	id := m.selectedID()
	if id == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var err error
	if action == "restart" {
		_, err = m.cl.ProcessService().Restart(ctx, id)
	} else {
		err = m.cl.ProcessService().Stop(ctx, id)
	}
	if err != nil {
		m.status = action + ": " + err.Error()
	} else {
		m.status = action + " ok"
	}
	m.refresh()
}

var tuiTitle = lipgloss.NewStyle().Bold(true).Render

func (m tuiModel) View() string {
	var sb strings.Builder
	sb.WriteString(tuiTitle("agent-runtime") + "  " + m.status + "\n")
	sb.WriteString("processes | logs  (j/k move, enter logs, r restart, s stop, esc back, q quit)\n\n")
	if m.mode == "processes" {
		for i, p := range m.procs {
			marker := " "
			if i == m.cursor {
				marker = ">"
			}
			fmt.Fprintf(&sb, "%s %-10v %-40v %v\n",
				marker, p["status"], truncate(fmt.Sprint(p["command"]), 40), p["pid"])
		}
		if len(m.procs) == 0 {
			sb.WriteString("(no processes — start one with: agent-runtime start --app <name>)\n")
		}
	} else {
		for _, l := range m.logs {
			sb.WriteString(truncate(l, max(20, m.width-2)) + "\n")
		}
	}
	return sb.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
