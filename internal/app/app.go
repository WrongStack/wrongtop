// Package app wires the wrongtop root model: global keys, tab routing and
// the outer layout (tab bar + content + status bar).
package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ersinkoc/wrongtop/internal/collector"
	"github.com/ersinkoc/wrongtop/internal/config"
	"github.com/ersinkoc/wrongtop/internal/dockerclient"
	"github.com/ersinkoc/wrongtop/internal/theme"
	"github.com/ersinkoc/wrongtop/internal/ui"
	"github.com/ersinkoc/wrongtop/internal/ui/dashboard"
	"github.com/ersinkoc/wrongtop/internal/ui/disks"
	"github.com/ersinkoc/wrongtop/internal/ui/docker"
	"github.com/ersinkoc/wrongtop/internal/ui/network"
	"github.com/ersinkoc/wrongtop/internal/ui/processes"
)

// Model is the bubbletea root model.
type Model struct {
	cfg       *config.Config
	theme     *theme.Theme
	version   string
	collector *collector.Collector
	docker    *dockerclient.Client

	width    int
	height   int
	active   int
	tabs     []ui.Tab
	helpMode bool
}

// New builds the root model with the tabs enabled by cfg.Modules.
func New(cfg *config.Config, version string) *Model {
	th := theme.ByName(cfg.Theme)
	// dashboard, disks and network are always present; cfg.Modules gates
	// the optional tabs.
	tabs := []ui.Tab{dashboard.New(cfg, th)}
	if cfg.Modules.Processes {
		tabs = append(tabs, processes.New(cfg, th))
	}
	if cfg.Modules.Docker {
		tabs = append(tabs, docker.New(cfg, th))
	}
	tabs = append(tabs, disks.New(cfg, th), network.New(cfg, th))

	m := &Model{
		cfg:       cfg,
		theme:     th,
		version:   version,
		collector: &collector.Collector{},
		tabs:      tabs,
	}
	return m
}

// Run starts the wrongtop TUI.
func Run(cfg *config.Config, version string) error {
	_, err := tea.NewProgram(New(cfg, version)).Run()
	return err
}

// tickMsg fires on every refresh interval.
type tickMsg time.Time

// dockerRetryMsg schedules a daemon reconnect attempt.
type dockerRetryMsg struct{}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.tickCmd(), m.collectCmd()}
	if m.cfg.Modules.Docker {
		cmds = append(cmds, m.connectDockerCmd())
	}
	return tea.Batch(cmds...)
}

func (m *Model) tickCmd() tea.Cmd {
	return tea.Every(m.cfg.Refresh.D(), func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) collectCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return collector.SnapshotMsg{Snap: m.collector.Collect(ctx)}
	}
}

func (m *Model) connectDockerCmd() tea.Cmd {
	return func() tea.Msg {
		c, err := dockerclient.New()
		return dockerclient.UpdateMsg{Client: c, Err: err}
	}
}

func (m *Model) dockerListCmd() tea.Cmd {
	client := m.docker
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		cons, err := client.List(ctx)
		return dockerclient.UpdateMsg{Client: client, Containers: cons, Err: err}
	}
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		for _, t := range m.tabs {
			t.SetSize(msg.Width, msg.Height-2) // tab bar + status bar
		}
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{m.tickCmd(), m.collectCmd()}
		if m.cfg.Modules.Docker && m.docker != nil {
			cmds = append(cmds, m.dockerListCmd())
		}
		return m, tea.Batch(cmds...)

	case dockerRetryMsg:
		return m, m.connectDockerCmd()

	case collector.SnapshotMsg:
		var cmds []tea.Cmd
		for _, t := range m.tabs { // hidden tabs keep their history buffers warm
			if cmd := t.Update(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)

	case dockerclient.UpdateMsg:
		if msg.Client != nil {
			m.docker = msg.Client
		}
		if msg.Client == nil && msg.Err != nil {
			// daemon absent or down — retry after a pause
			return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg {
				return dockerRetryMsg{}
			})
		}
		var cmds []tea.Cmd
		for _, t := range m.tabs {
			if cmd := t.Update(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)

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
	case "?":
		m.helpMode = !m.helpMode
		return nil, true
	case "esc":
		if m.helpMode {
			m.helpMode = false
			return nil, true
		}
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
	content := m.tabs[m.active].View()
	if m.helpMode {
		content = m.helpOverlay(content)
	}
	v := tea.NewView(strings.Join([]string{
		m.tabBarView(),
		content,
		m.statusBarView(),
	}, "\n"))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "wrongtop"
	return v
}

// helpOverlay centers the help box over the tab content.
func (m *Model) helpOverlay(content string) string {
	return lipgloss.JoinVertical(lipgloss.Center,
		lipgloss.Place(m.width, strings.Count(content, "\n")+1,
			lipgloss.Center, lipgloss.Center, m.helpView()),
	)
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
	right := m.theme.Styles.HelpKey.Render(fmt.Sprintf("1-%d", len(m.tabs))) +
		m.theme.Styles.HelpText.Render(" tabs  ") +
		m.theme.Styles.HelpKey.Render("tab") +
		m.theme.Styles.HelpText.Render(" next  ") +
		m.theme.Styles.HelpKey.Render("?") +
		m.theme.Styles.HelpText.Render(" help  ") +
		m.theme.Styles.HelpKey.Render("q") +
		m.theme.Styles.HelpText.Render(" quit")

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return m.theme.Styles.Status.Render(left + strings.Repeat(" ", gap) + right)
}
