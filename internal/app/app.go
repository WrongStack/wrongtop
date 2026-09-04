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
	"github.com/ersinkoc/wrongtop/internal/format"
	"github.com/ersinkoc/wrongtop/internal/remote"
	"github.com/ersinkoc/wrongtop/internal/theme"
	"github.com/ersinkoc/wrongtop/internal/ui"
	"github.com/ersinkoc/wrongtop/internal/ui/canvas"
	"github.com/ersinkoc/wrongtop/internal/ui/dashboard"
	"github.com/ersinkoc/wrongtop/internal/ui/disks"
	"github.com/ersinkoc/wrongtop/internal/ui/docker"
	"github.com/ersinkoc/wrongtop/internal/ui/network"
	"github.com/ersinkoc/wrongtop/internal/ui/processes"
)

// alertEvent is one threshold crossing in the session alert history.
type alertEvent struct {
	Key   string
	Text  string
	Crit  bool
	Start time.Time
	End   time.Time // zero while ongoing
}

// alertHistory caps the in-memory alert ring.
const alertHistory = 256

// cpuSparkSamples is the history length of the status-bar cpu sparkline.
const cpuSparkSamples = 14

// flashTTL is how long a transient status-bar note stays visible.
const flashTTL = 3 * time.Second

// Model is the bubbletea root model.
type Model struct {
	cfg       *config.Config
	cfgPath   string
	theme     *theme.Theme
	version   string
	collector *collector.Collector
	docker    *dockerclient.Client

	width, height int
	active        int
	tabs          []ui.Tab
	helpMode      bool
	alertsMode    bool // alert-history overlay

	alerts     []*alertEvent
	openAlerts map[string]*alertEvent

	// cpu history for the status-bar sparkline (percent / 100 samples)
	cpuHist []float64

	flash      string // transient status-bar note
	flashUntil time.Time

	// remote monitoring: when stream is set, snapshots arrive from a
	// server and local collection is disabled entirely.
	stream     *remote.Client
	remoteAddr string

	latest collector.Snapshot // most recent sample, for the status bar
}

// NewRemote builds the root model fed by a remote snapshot stream.
// Docker is unavailable remotely and the view is read-only.
func NewRemote(cfg *config.Config, cfgPath, version string, stream *remote.Client, addr string) *Model {
	cfg.Modules.Docker = false
	m := New(cfg, cfgPath, version)
	m.stream = stream
	m.remoteAddr = addr
	return m
}

// RunRemote starts the wrongtop TUI fed by a remote stream; a reader
// goroutine pushes snapshots into the event loop.
func RunRemote(cfg *config.Config, cfgPath, version string, stream *remote.Client, addr string) error {
	program := tea.NewProgram(NewRemote(cfg, cfgPath, version, stream, addr))
	go func() {
		for {
			snap, err := stream.Next()
			if err != nil {
				program.Send(remoteEndedMsg{err})
				return
			}
			program.Send(collector.SnapshotMsg{Snap: snap})
		}
	}()
	_, err := program.Run()
	return err
}

// remoteEndedMsg signals that the remote stream terminated.
type remoteEndedMsg struct{ err error }

// New builds the root model with the tabs enabled by cfg.Modules.
func New(cfg *config.Config, cfgPath, version string) *Model {
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

	return &Model{
		cfg:        cfg,
		cfgPath:    cfgPath,
		theme:      th,
		version:    version,
		collector:  collector.New(cfg.Refresh.D()),
		tabs:       tabs,
		openAlerts: make(map[string]*alertEvent),
	}
}

// Run starts the wrongtop TUI.
func Run(cfg *config.Config, cfgPath, version string) error {
	_, err := tea.NewProgram(New(cfg, cfgPath, version)).Run()
	return err
}

// tickMsg fires on every refresh interval.
type tickMsg time.Time

// dockerRetryMsg schedules a daemon reconnect attempt.
type dockerRetryMsg struct{}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	if m.stream != nil {
		return nil // snapshots arrive from the remote reader goroutine
	}
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
		if m.stream != nil {
			return m, nil // remote mode: no local polling
		}
		cmds := []tea.Cmd{m.tickCmd(), m.collectCmd()}
		if m.cfg.Modules.Docker && m.docker != nil {
			cmds = append(cmds, m.dockerListCmd())
		}
		return m, tea.Batch(cmds...)

	case remoteEndedMsg:
		if msg.err != nil {
			m.setFlash("remote stream ended: " + msg.err.Error())
		} else {
			m.setFlash("remote stream ended")
		}
		return m, nil

	case dockerRetryMsg:
		return m, m.connectDockerCmd()

	case collector.SnapshotMsg:
		m.latest = msg.Snap
		m.trackAlerts(msg.Snap)
		pct := min(max(m.latest.CPU.Percent/100, 0), 1)
		m.cpuHist = append(m.cpuHist, pct)
		if len(m.cpuHist) > cpuSparkSamples {
			m.cpuHist = m.cpuHist[len(m.cpuHist)-cpuSparkSamples:]
		}
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

	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Y == 0 { // tab bar row
			if mouse.Button == tea.MouseLeft {
				if n, ok := m.tabAt(mouse.X); ok {
					m.active = n
				}
			}
			return m, nil
		}
		if !m.helpMode && !m.alertsMode {
			return m, m.tabs[m.active].Update(msg)
		}

	case tea.KeyPressMsg:
		if cmd, handled := m.globalKey(msg); handled {
			return m, cmd
		}
		if m.helpMode || m.alertsMode {
			return m, nil // overlays swallow other keys
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
	case "a":
		m.alertsMode = !m.alertsMode
		return nil, true
	case "esc":
		if m.helpMode {
			m.helpMode = false
			return nil, true
		}
		if m.alertsMode {
			m.alertsMode = false
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
	case "T":
		m.cycleTheme()
		return nil, true
	case "R":
		m.reloadConfig()
		return nil, true
	}
	return nil, false
}

// cycleTheme switches to the next theme in theme.Names() live, without
// touching the config file.
func (m *Model) cycleTheme() {
	names := theme.Names()
	next := names[0]
	for i, name := range names {
		if name == m.cfg.Theme {
			next = names[(i+1)%len(names)]
			break
		}
	}
	m.applyTheme(next)
	m.setFlash("theme: " + next)
}

// applyTheme resolves and distributes a theme to every tab.
func (m *Model) applyTheme(name string) {
	th := theme.ByName(name)
	m.theme = th
	m.cfg.Theme = name
	for _, t := range m.tabs {
		t.SetTheme(th)
	}
}

// reloadConfig re-reads the YAML file and applies everything that can
// change live: theme, refresh cadence, thresholds and key bindings.
func (m *Model) reloadConfig() {
	reloaded, err := config.Load(m.cfgPath)
	if err != nil {
		m.setFlash("config error: " + err.Error())
		return
	}
	*m.cfg = *reloaded // tabs share the pointer; in-place swap updates all
	th := theme.ByName(m.cfg.Theme)
	m.theme = th
	for _, t := range m.tabs {
		t.SetTheme(th)
	}
	m.setFlash("config reloaded")
}

func (m *Model) setFlash(text string) {
	m.flash = text
	m.flashUntil = time.Now().Add(flashTTL)
}

// trackAlerts folds the snapshot's threshold crossings into the session
// history: new keys open events, disappeared keys close them.
func (m *Model) trackAlerts(snap collector.Snapshot) {
	current := dashboard.EvaluateAlerts(m.cfg, snap)
	seen := make(map[string]bool, len(current))
	now := snap.Time
	for _, a := range current {
		seen[a.Key] = true
		if ev, ok := m.openAlerts[a.Key]; ok {
			ev.Text = a.Text
			ev.Crit = a.Crit // severity may escalate mid-event
			continue
		}
		ev := &alertEvent{Key: a.Key, Text: a.Text, Crit: a.Crit, Start: now}
		m.openAlerts[a.Key] = ev
		m.alerts = append(m.alerts, ev)
	}
	for key, ev := range m.openAlerts {
		if !seen[key] {
			ev.End = now
			delete(m.openAlerts, key)
		}
	}
	if len(m.alerts) > alertHistory {
		m.alerts = m.alerts[len(m.alerts)-alertHistory:]
	}
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	content := m.tabs[m.active].View()
	if m.helpMode {
		content = m.helpOverlay(content)
	}
	if m.alertsMode {
		content = m.alertsOverlay(content)
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
			lipgloss.Center, lipgloss.Center, ui.Shadow(m.helpView(), m.theme.Styles.Muted)),
	)
}

// alertsOverlay centers the alert-history box over the tab content.
func (m *Model) alertsOverlay(content string) string {
	return lipgloss.JoinVertical(lipgloss.Center,
		lipgloss.Place(m.width, strings.Count(content, "\n")+1,
			lipgloss.Center, lipgloss.Center, ui.Shadow(m.alertsOverlayView(), m.theme.Styles.Muted)),
	)
}

func (m *Model) alertsOverlayView() string {
	st := m.theme.Styles
	var b strings.Builder
	if len(m.alerts) == 0 {
		b.WriteString(st.Muted.Render("no threshold crossings this session"))
	}
	now := time.Now()
	for i := len(m.alerts) - 1; i >= 0 && i > len(m.alerts)-17; i-- { // newest first
		ev := m.alerts[i]
		style := st.Warn
		if ev.Crit {
			style = st.Crit
		}
		marker, dur := "●", ev.End.Sub(ev.Start)
		if ev.End.IsZero() {
			dur = now.Sub(ev.Start)
		} else {
			marker = "·"
		}
		b.WriteString(ev.Start.Format("15:04:05") + "  " +
			style.Render(marker+" "+ev.Text) +
			st.Muted.Render(fmt.Sprintf("  [%s]", dur.Round(time.Second))))
		if i < len(m.alerts) && b.Len() > 0 {
			b.WriteString("\n")
		}
	}
	footer := st.HelpKey.Render("esc") + st.HelpText.Render(" close")
	return ui.Box(lipgloss.RoundedBorder(), st.Border, st.BorderChar, st.BorderTitle,
		"ALERTS", strings.TrimRight(b.String(), "\n")+"\n\n  "+footer)
}

// tabAt resolves a click on the tab bar row to a tab index. Tab styles
// pad each label with one blank column on either side.
func (m *Model) tabAt(x int) (int, bool) {
	start := 0
	for i, t := range m.tabs {
		w := lipgloss.Width(t.Title()) + 2
		if x >= start && x < start+w {
			return i, true
		}
		start += w
	}
	return 0, false
}

// tabBarView renders the tab strip.
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

// statusBarView renders the btop-style bottom line: an identity chip
// left, live chips center, key hints right.
func (m *Model) statusBarView() string {
	pal := m.theme.Palette
	logo := lipgloss.NewStyle().Background(lipgloss.Color(pal.Purple)).
		Foreground(lipgloss.Color(pal.BG)).Bold(true).
		Padding(0, 1).Render("WRONGTOP")
	left := logo + " " + m.theme.Styles.Muted.Render("v"+m.version)
	if m.stream != nil {
		left += " " + lipgloss.NewStyle().Background(lipgloss.Color(pal.Orange)).
			Foreground(lipgloss.Color(pal.BG)).Bold(true).
			Padding(0, 1).Render("REMOTE "+m.remoteAddr)
	}

	right := m.theme.Styles.HelpKey.Render(fmt.Sprintf("1-%d", len(m.tabs))) +
		m.theme.Styles.HelpText.Render(" tabs  ") +
		m.theme.Styles.HelpKey.Render("T") +
		m.theme.Styles.HelpText.Render(" theme  ") +
		m.theme.Styles.HelpKey.Render("R") +
		m.theme.Styles.HelpText.Render(" reload  ") +
		m.theme.Styles.HelpKey.Render("a") +
		m.theme.Styles.HelpText.Render(" alerts  ") +
		m.theme.Styles.HelpKey.Render("?") +
		m.theme.Styles.HelpText.Render(" help  ") +
		m.theme.Styles.HelpKey.Render("q") +
		m.theme.Styles.HelpText.Render(" quit")

	mid := ""
	if time.Now().Before(m.flashUntil) && m.flash != "" {
		mid = lipgloss.NewStyle().Background(lipgloss.Color(pal.Yellow)).
			Foreground(lipgloss.Color(pal.BG)).Bold(true).
			Padding(0, 1).Render(m.flash)
	} else if !m.latest.Time.IsZero() {
		mid = m.liveSummary()
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - lipgloss.Width(mid)
	if gap < 1 {
		gap = 1
	}
	half := gap / 2
	return m.theme.Styles.Status.Render(left +
		strings.Repeat(" ", half) + mid + strings.Repeat(" ", gap-half) + right)
}

// chip renders a status-bar segment with a solid background, btop-style.
func (m *Model) chip(bg, text string) string {
	return lipgloss.NewStyle().Background(lipgloss.Color(bg)).
		Foreground(lipgloss.Color(m.theme.Palette.BG)).Padding(0, 1).Render(text)
}

// liveSummary renders the center chips: cpu, memory, network rates and,
// when the platform reports them, temperature and battery.
func (m *Model) liveSummary() string {
	pal := m.theme.Palette
	t := m.cfg.Thresholds

	bgFor := func(warn, crit, v float64) string {
		switch {
		case v >= crit:
			return pal.Red
		case v >= warn:
			return pal.Yellow
		default:
			return pal.Green
		}
	}
	var b strings.Builder
	spark := canvas.Sparkline(m.cpuHist, canvas.Ramp{pal.BG}) // uncolored inside the chip
	b.WriteString(m.chip(bgFor(t.CPUWarn, t.CPUCrit, m.latest.CPU.Percent),
		"cpu "+spark+fmt.Sprintf(" %.0f%%", m.latest.CPU.Percent)))
	b.WriteString(" ")
	b.WriteString(m.chip(bgFor(t.MemWarn, t.MemCrit, m.latest.Mem.Percent),
		fmt.Sprintf("mem %.0f%%", m.latest.Mem.Percent)))

	var rx, tx float64
	for _, n := range m.latest.Nets {
		rx += n.RxRate
		tx += n.TxRate
	}
	b.WriteString(" ")
	b.WriteString(m.chip(pal.Blue, fmt.Sprintf("↓%s ↑%s", format.Rate(rx), format.Rate(tx))))

	if len(m.latest.Sensors) > 0 {
		s := m.latest.Sensors[0]
		b.WriteString(" ")
		b.WriteString(m.chip(bgFor(t.TempWarn, t.TempCrit, s.TempC),
			fmt.Sprintf("%.0f°C", s.TempC)))
	}
	if bat := m.latest.Battery; bat != nil {
		icon := "bat"
		if bat.Charging {
			icon = "⚡"
		}
		bg := pal.Green
		if !bat.Charging && bat.Percent <= 30 {
			bg = pal.Red
		}
		b.WriteString(" ")
		b.WriteString(m.chip(bg, fmt.Sprintf("%s%.0f%%", icon, bat.Percent)))
	}
	return b.String()
}
