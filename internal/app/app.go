// Package app wires the wrongtop root model: global keys, tab routing and
// the outer layout (tab bar + content + status bar).
package app

import (
	"strings"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ersinkoc/wrongtop/internal/config"
	"github.com/ersinkoc/wrongtop/internal/theme"
	"github.com/ersinkoc/wrongtop/internal/ui"
)

// Model is the bubbletea root model.
type Model struct {
	cfg     *config.Config
	theme   *theme.Theme
	version string

	width  int
	height int
	active int
	tabs   []ui.Tab
}

// New builds the root model with all tabs attached.
func New(cfg *config.Config, version string) *Model {
	th := theme.ByName(cfg.Theme)
	m := &Model{
		cfg:     cfg,
		theme:   th,
		version: version,
		tabs: []ui.Tab{
			ui.NewPlaceholder("DASHBOARD", "cpu · memory · host metrics — phase 2"),
			ui.NewPlaceholder("PROCESSES", "process table, sort / filter / kill — phase 3"),
			ui.NewPlaceholder("DOCKER", "containers, stats and actions — phase 5"),
			ui.NewPlaceholder("DISKS", "filesystems and I/O rates — phase 4"),
			ui.NewPlaceholder("NETWORK", "per-interface traffic — phase 4"),
		},
	}
	return m
}

// Run starts the wrongtop TUI.
func Run(cfg *config.Config, version string) error {
	_, err := tea.NewProgram(New(cfg, version)).Run()
	return err
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		for _, t := range m.tabs {
			t.SetSize(msg.Width, msg.Height-2) // tab bar + status bar
		}
		return m, nil

	case tea.KeyPressMsg:
		if cmd, handled := m.globalKey(msg); handled {
			return m, cmd
		}
		return m, m.tabs[m.active].Update(msg)
	}

	return m, m.tabs[m.active].Update(msg)
}

// globalKey handles keys that work on every tab. It reports whether the
// key was consumed.
func (m *Model) globalKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	switch key.String() {
	case "q", "ctrl+c":
		return tea.Quit, true
	case "tab":
		m.active = (m.active + 1) % len(m.tabs)
		return nil, true
	case "shift+tab":
		m.active = (m.active - 1 + len(m.tabs)) % len(m.tabs)
		return nil, true
	case "1", "2", "3", "4", "5":
		if n := int(key.String()[0] - '1'); n < len(m.tabs) {
			m.active = n
		}
		return nil, true
	}
	return nil, false
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	v := tea.NewView(strings.Join([]string{
		m.tabBarView(),
		m.tabs[m.active].View(),
		m.statusBarView(),
	}, "\n"))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "wrongtop"
	return v
}

func (m *Model) tabBarView() string {
	parts := make([]string, len(m.tabs))
	for i, t := range m.tabs {
		label := t.Title()
		if i == m.active {
			parts[i] = m.theme.Styles.TabActive.Render(label)
			continue
		}
		parts[i] = m.theme.Styles.TabInactive.Render(label)
	}
	return m.theme.Styles.TabBar.Render(strings.Join(parts, ""))
}

func (m *Model) statusBarView() string {
	left := m.theme.Styles.Title.Render("wrongtop ") +
		m.theme.Styles.Muted.Render("v"+m.version)
	right := m.theme.Styles.HelpKey.Render("1-5") +
		m.theme.Styles.HelpText.Render(" tabs  ") +
		m.theme.Styles.HelpKey.Render("tab") +
		m.theme.Styles.HelpText.Render(" next  ") +
		m.theme.Styles.HelpKey.Render("q") +
		m.theme.Styles.HelpText.Render(" quit")

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return m.theme.Styles.Status.Render(left + strings.Repeat(" ", gap) + right)
}
