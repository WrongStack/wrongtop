// Package app wires the wrongtop root model: global keys, tab routing and
// the outer layout (tab bar + content + status bar).
package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/dockerclient"
	"github.com/wrongstack/wrongtop/internal/format"
	"github.com/wrongstack/wrongtop/internal/remote"
	"github.com/wrongstack/wrongtop/internal/theme"
	"github.com/wrongstack/wrongtop/internal/ui"
	"github.com/wrongstack/wrongtop/internal/ui/canvas"
	"github.com/wrongstack/wrongtop/internal/ui/conns"
	"github.com/wrongstack/wrongtop/internal/ui/dashboard"
	"github.com/wrongstack/wrongtop/internal/ui/disks"
	"github.com/wrongstack/wrongtop/internal/ui/docker"
	"github.com/wrongstack/wrongtop/internal/ui/network"
	"github.com/wrongstack/wrongtop/internal/ui/processes"
	"github.com/wrongstack/wrongtop/internal/ui/sensors"
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

// dockerRetryDelay is the daemon-reconnect backoff; a var so tests can
// shorten the wait.
var dockerRetryDelay = 5 * time.Second

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

	// tab bar geometry, recorded by tabBarView so mouse hits map onto
	// whatever style the tabs currently render with
	tabBounds      [][2]int // clickable [start,end) of each tab
	alertZoneStart int      // left edge of the alert/clock zone; -1 = none

	alerts     []*alertEvent
	openAlerts map[string]*alertEvent

	pulse bool // flips per snapshot: drives the critical chip pulse

	// cpu history for the status-bar sparkline (percent / 100 samples)
	cpuHist []float64

	// the composed screen is cached: only messages that can change it
	// mark it dirty. Mouse-motion floods (drag across the window fires
	// hundreds per second) reuse the string, and bubbletea's renderer
	// then skips the diff entirely because the frame is byte-identical.
	frameCache string

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
	cfg.ReadOnly = true // remote pids are not local pids: no signaling, no local cmdline
	m := New(cfg, cfgPath, version)
	m.stream = stream
	m.remoteAddr = addr
	return m
}

// programOptions are applied to every bubbletea program wrongtop starts;
// tests swap in a headless input/output so Run executes end to end.
var programOptions []tea.ProgramOption

// RunRemote starts the wrongtop TUI fed by a remote stream; a reader
// goroutine pushes snapshots into the event loop.
func RunRemote(cfg *config.Config, cfgPath, version string, stream *remote.Client, addr string) error {
	program := tea.NewProgram(NewRemote(cfg, cfgPath, version, stream, addr), programOptions...)
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
	// the optional tabs (new tabs append at the end so key bindings stay
	// stable across upgrades).
	tabs := []ui.Tab{dashboard.New(cfg, th)}
	if cfg.Modules.Processes {
		tabs = append(tabs, processes.New(cfg, th))
	}
	if cfg.Modules.Docker {
		tabs = append(tabs, docker.New(cfg, th))
	}
	tabs = append(tabs, disks.New(cfg, th), network.New(cfg, th))
	if cfg.Modules.Sensors {
		tabs = append(tabs, sensors.New(cfg, th))
	}
	if cfg.Modules.Connections {
		tabs = append(tabs, conns.New(cfg, th))
	}

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

// setActive switches the active tab and updates visibility flags so
// hidden tabs can skip their per-snapshot rebuilds. Tab constructors
// default to visible; the dashboard (index 0) is the only one that
// starts active.
func (m *Model) setActive(i int) {
	if i < 0 || i >= len(m.tabs) || i == m.active {
		return
	}
	m.tabs[m.active].SetVisible(false)
	m.active = i
	m.tabs[i].SetVisible(true)
}

// Run starts the wrongtop TUI.
func Run(cfg *config.Config, cfgPath, version string) error {
	_, err := tea.NewProgram(New(cfg, cfgPath, version), programOptions...).Run()
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
	if _, motion := msg.(tea.MouseMotionMsg); motion {
		return m, nil // drag noise: nothing reads hover position
	}
	m.frameCache = "" // every other message may change the frame
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
		m.pulse = !m.pulse // the crit chip breathes at the refresh cadence
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
			return m, tea.Tick(dockerRetryDelay, func(time.Time) tea.Msg {
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
					m.setActive(n)
				} else if m.alertZoneStart >= 0 && mouse.X >= m.alertZoneStart {
					m.alertsMode = !m.alertsMode // alert chips + clock zone
				}
			}
			return m, nil
		}
		if !m.helpMode && !m.alertsMode {
			return m, m.tabs[m.active].Update(msg)
		}
		return m, nil // overlays swallow content clicks, like keys

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
		m.setActive((m.active + 1) % len(m.tabs))
		return nil, true
	case "shift+tab":
		m.setActive((m.active - 1 + len(m.tabs)) % len(m.tabs))
		return nil, true
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if n := int(key.String()[0] - '1'); n < len(m.tabs) {
			m.setActive(n)
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
	if m.frameCache == "" {
		m.frameCache = m.frame()
	}
	v := tea.NewView(m.frameCache)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = m.windowTitle()
	return v
}

// windowTitle carries the at-a-glance figures into the terminal window —
// visible even when wrongtop is in a background tab.
func (m *Model) windowTitle() string {
	if m.latest.Time.IsZero() {
		return "wrongtop"
	}
	return fmt.Sprintf("wrongtop · cpu %.0f%% · mem %.0f%%",
		m.latest.CPU.Percent, m.latest.Mem.Percent)
}

// frame composes the full screen: tab bar, content area, status bar.
// The content area is padded (or its trailing blanks trimmed) so it is
// exactly height-2 rows — a short tab (empty docker, sparse sensors,
// waiting for the first snapshot) can never pull the status bar off the
// bottom row. It is docked, always.
func (m *Model) frame() string {
	content := m.tabs[m.active].View()
	if m.helpMode {
		content = m.helpOverlay(content)
	}
	if m.alertsMode {
		content = m.alertsOverlay(content)
	}

	lines := strings.Split(content, "\n")
	for len(lines) > 0 && strings.TrimSpace(stripANSI(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1] // trailing blanks carry no information
	}
	for len(lines) < m.height-2 {
		lines = append(lines, "")
	}

	return strings.Join([]string{
		m.tabBarView(),
		strings.Join(lines, "\n"),
		m.statusBarView(),
	}, "\n")
}

// stripANSI removes SGR (and other escape) sequences, leaving printable
// text — used to tell styled-blank lines from real content.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
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
		line := ev.Start.Format("15:04:05") + "  " +
			style.Render(marker+" "+ev.Text) +
			st.Muted.Render(fmt.Sprintf("  [%s]", dur.Round(time.Second)))
		// Event text carries snapshot values (the remote wire can
		// deliver extremes), so bound each history line to the
		// terminal — the same containment the tab-bar chips and the
		// status bar apply.
		if limit := m.width - 6; limit > 20 { // box borders + shadow margin
			line = ansi.Truncate(line, limit, "")
		}
		b.WriteString(line)
		if b.Len() > 0 {
			b.WriteString("\n")
		}
	}
	footer := st.HelpKey.Render("esc") + st.HelpText.Render(" close")
	return ui.Box(ui.BorderFor(m.cfg.Border), st.Border, st.BorderChar, st.BorderTitle,
		ui.Icon("warn", m.cfg.NerdFonts)+" ALERTS",
		st.Muted.Render(fmt.Sprintf("%d events ", len(m.alerts))),
		strings.TrimRight(b.String(), "\n")+"\n\n  "+footer)
}

// tabAt resolves a click on the tab bar row to a tab index. Bounds are
// recorded by tabBarView, so any tab style (padding, powerline caps)
// stays clickable without duplicating width math.
func (m *Model) tabAt(x int) (int, bool) {
	for i, b := range m.tabBounds {
		if x >= b[0] && x < b[1] {
			return i, true
		}
	}
	return 0, false
}

// tabBarView renders the tab strip: tabs left; active alert chips and a
// clock right. The alert zone is app chrome — glances-style warnings
// that stay visible on every tab and never reflow the dashboard grid.
// Under width pressure tabs collapse to their icons first, then the
// chips degrade to a bare count, then yield the row entirely.
func (m *Model) tabBarView() string {
	left := m.tabBarLeft(false)
	right := m.tabBarRight()
	if lipgloss.Width(left)+lipgloss.Width(right) > m.width {
		if compact := m.tabBarLeft(true); lipgloss.Width(compact)+lipgloss.Width(right) <= m.width {
			left = compact // inactive tabs shrink to their icons
		} else {
			m.tabBarLeft(false) // compact not adopted: restore full-width bounds
		}
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 { // a bare alert count, then nothing
		if n := len(m.openAlerts); n > 0 {
			right = m.chip(m.theme.Palette.Red, ui.Icon("warn", m.cfg.NerdFonts)+fmt.Sprintf(" %d", n))
			gap = m.width - lipgloss.Width(left) - lipgloss.Width(right)
		}
	}
	m.alertZoneStart = -1
	if gap < 1 {
		right = "" // the tabs win; alerts stay on `a` and the status bar
		gap = max(0, m.width-lipgloss.Width(left))
	} else {
		m.alertZoneStart = m.width - lipgloss.Width(right)
	}
	return left + strings.Repeat(" ", gap) + right
}

// tabBarLeft renders the tab strip and records the clickable bounds.
// With compact set, inactive tabs drop their labels and keep only the
// icon — the first degradation step before the row gives up.
func (m *Model) tabBarLeft(compact bool) string {
	st := m.theme.Styles
	pal := m.theme.Palette
	parts := make([]string, len(m.tabs))
	m.tabBounds = make([][2]int, len(m.tabs))
	x := 0
	for i, t := range m.tabs {
		label := t.Title()
		if compact && i != m.active { // the active tab always keeps its label
			if icon, _, found := strings.Cut(label, " "); found {
				label = icon
			}
		}
		var part string
		if i == m.active {
			part = st.TabActive.Render(label)
			if m.cfg.NerdFonts { // powerline cap on the active chip
				part += lipgloss.NewStyle().
					Foreground(lipgloss.Color(m.theme.Soft(pal.Purple))).
					Background(lipgloss.Color(pal.BG)).
					Render("")
			}
		} else {
			part = st.TabInactive.Render(label)
		}
		w := lipgloss.Width(part)
		m.tabBounds[i] = [2]int{x, x + w}
		x += w
		parts[i] = part
	}
	return st.TabBar.Render(strings.Join(parts, ""))
}

// tabBarRight renders the alert/clock zone: active alerts (critical
// first, at most three chips) followed by the clock. Critical chips
// pulse by blending two soft fills on alternate snapshots, and long
// events carry their duration so a stuck condition reads at a glance.
func (m *Model) tabBarRight() string {
	pal := m.theme.Palette
	events := make([]*alertEvent, 0, len(m.openAlerts))
	for _, ev := range m.openAlerts {
		events = append(events, ev)
	}
	sort.Slice(events, func(i, j int) bool { // crit leads, then oldest
		if events[i].Crit != events[j].Crit {
			return events[i].Crit
		}
		return events[i].Start.Before(events[j].Start)
	})
	var chips []string
	for _, ev := range events[:min(3, len(events))] {
		bg, blend := pal.Yellow, 0.55
		if ev.Crit {
			bg = pal.Red
			if m.pulse {
				blend = 0.80 // brighter on alternate ticks: alive, not painted over
			}
		}
		text := ui.Icon("warn", m.cfg.NerdFonts) + " " + ev.Text
		if d := time.Since(ev.Start); d > time.Minute {
			text += " · " + format.Uptime(d)
		}
		chips = append(chips, m.chipT(bg, text, blend))
	}
	if n := len(events) - 3; n > 0 {
		chips = append(chips, m.chip(pal.Red, ui.Icon("warn", m.cfg.NerdFonts)+fmt.Sprintf(" +%d", n)))
	}
	chips = append(chips, m.theme.Styles.Muted.Render(time.Now().Format("15:04")))
	return strings.Join(chips, " ")
}

// statusBarView renders the btop-style bottom line: an identity chip
// left, live chips center, key hints right. Under width pressure the
// hints collapse to the essentials and the lowest-priority chip drops
// (battery → temperature → network) so the line never wraps.
func (m *Model) statusBarView() string {
	pal := m.theme.Palette
	logo := m.chip(pal.Purple, "WRONGTOP")
	left := logo + " " + m.theme.Styles.Muted.Render("v"+m.version)
	if m.stream != nil {
		left += " " + m.chip(pal.Orange, "REMOTE "+m.remoteAddr)
	}

	right := m.statusHints(true)
	if lipgloss.Width(left)+lipgloss.Width(right) > m.width {
		right = m.statusHints(false)
	}

	mid := ""
	if time.Now().Before(m.flashUntil) && m.flash != "" {
		mid = m.chip(pal.Yellow, m.flash)
	} else if !m.latest.Time.IsZero() {
		chips := m.liveChips()
		sep := m.chipSep()
		for len(chips) > 0 &&
			lipgloss.Width(left)+lipgloss.Width(right)+lipgloss.Width(strings.Join(chips, sep)) > m.width {
			chips = chips[:len(chips)-1] // lowest priority drops first
		}
		mid = strings.Join(chips, sep)
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - lipgloss.Width(mid)
	if gap < 1 {
		gap = 1
	}
	half := gap / 2
	return m.theme.Styles.Status.Render(left +
		strings.Repeat(" ", half) + mid + strings.Repeat(" ", gap-half) + right)
}

// statusHints renders the right-hand key hints; the short form appears
// when the terminal is too narrow for the full list.
func (m *Model) statusHints(full bool) string {
	st := m.theme.Styles
	if !full {
		return st.HelpKey.Render("?") + st.HelpText.Render(" help  ") +
			st.HelpKey.Render("q") + st.HelpText.Render(" quit")
	}
	return st.HelpKey.Render(fmt.Sprintf("1-%d", len(m.tabs))) +
		st.HelpText.Render(" tabs  ") +
		st.HelpKey.Render("T") + st.HelpText.Render(" theme  ") +
		st.HelpKey.Render("R") + st.HelpText.Render(" reload  ") +
		st.HelpKey.Render("a") + st.HelpText.Render(" alerts  ") +
		st.HelpKey.Render("?") + st.HelpText.Render(" help  ") +
		st.HelpKey.Render("q") + st.HelpText.Render(" quit")
}

// chipSep is the separator between status chips: a powerline arrow when
// nerd fonts are on, a blank column otherwise.
func (m *Model) chipSep() string {
	if m.cfg.NerdFonts {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.Gray)).
			Background(lipgloss.Color(m.theme.Palette.BG)).
			Render("")
	}
	return " "
}

// chip renders a status-bar segment with a softened background, modern
// soft-UI style: solid primaries read harsh in large fills.
func (m *Model) chip(bg, text string) string {
	return m.chipT(bg, text, 0.55)
}

// chipT renders a chip whose fill blends toward the background at t —
// higher t reads louder. The crit pulse lives on that dial.
func (m *Model) chipT(bg, text string, t float64) string {
	return lipgloss.NewStyle().Background(lipgloss.Color(canvas.Ramp{m.theme.Palette.BG, bg}.At(t))).
		Foreground(lipgloss.Color(m.theme.Palette.FG)).Padding(0, 1).Render(text)
}

// liveChips renders the center chips in drop-priority order: cpu,
// memory, network rates, temperature, battery. Figures use fixed-width
// rate formatting so the bar doesn't jitter as magnitudes cross units.
func (m *Model) liveChips() []string {
	pal := m.theme.Palette
	t := m.cfg.Thresholds
	nerd := m.cfg.NerdFonts
	icon := func(kind, label string) string {
		if nerd {
			return ui.Icon(kind, true) + " "
		}
		return label + " "
	}

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
	var chips []string
	spark := canvas.Sparkline(m.cpuHist, canvas.Ramp{pal.BG}) // uncolored inside the chip
	chips = append(chips, m.chip(bgFor(t.CPUWarn, t.CPUCrit, m.latest.CPU.Percent),
		icon("cpu", "cpu")+spark+fmt.Sprintf(" %.0f%%", m.latest.CPU.Percent)))
	chips = append(chips, m.chip(bgFor(t.MemWarn, t.MemCrit, m.latest.Mem.Percent),
		icon("mem", "mem")+fmt.Sprintf("%.0f%%", m.latest.Mem.Percent)))

	var rx, tx float64
	for _, n := range m.latest.Nets {
		rx += n.RxRate
		tx += n.TxRate
	}
	chips = append(chips, m.chip(pal.Blue, fmt.Sprintf("↓%s ↑%s",
		shortRateFixed(rx), shortRateFixed(tx))))

	if len(m.latest.Sensors) > 0 {
		s := m.latest.Sensors[0]
		label := fmt.Sprintf("%.0f°C", s.TempC)
		if g := ui.Icon("temp", nerd); g != "" {
			label = g + " " + label
		}
		chips = append(chips, m.chip(bgFor(t.TempWarn, t.TempCrit, s.TempC), label))
	}
	if bat := m.latest.Battery; bat != nil {
		label := icon("bat", "bat")
		if bat.Charging {
			label = ui.Icon("bolt", nerd) + " " // the charging bolt
		}
		bg := pal.Green
		if !bat.Charging && bat.Percent <= 30 {
			bg = pal.Red
		}
		chips = append(chips, m.chip(bg, fmt.Sprintf("%s%.0f%%", label, bat.Percent)))
	}
	return chips
}

// shortRateFixed renders a rate without the "/s" suffix, padded to a
// stable width so chip pairs stay put between refreshes.
func shortRateFixed(bps float64) string {
	s := strings.TrimSuffix(format.Rate(bps), "/s")
	for len(s) < 9 { // rates are pure ASCII: len == display width
		s += " "
	}
	return s
}
