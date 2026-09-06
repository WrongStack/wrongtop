// Package docker renders the Docker tab: container list with live stats,
// lifecycle actions and a follow-mode log viewer.
package docker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/dockerclient"
	"github.com/wrongstack/wrongtop/internal/format"
	"github.com/wrongstack/wrongtop/internal/theme"
	"github.com/wrongstack/wrongtop/internal/ui"
	"github.com/wrongstack/wrongtop/internal/ui/canvas"
)

// dockerLogLines caps the in-memory log buffer.
const dockerLogLines = 1000

// Model is the docker tab.
type Model struct {
	cfg *config.Config
	th  *theme.Theme

	width, height int
	table         table.Model

	client *dockerclient.Client
	cons   []dockerclient.Container
	err    error // last poll/connect error

	// rate computation: docker reports lifetime counters, so rates come
	// from diffing consecutive polls (first sighting yields zeros)
	prev     map[string]dockerclient.Container // by container ID
	prevTime time.Time
	rates    map[string][4]float64 // ↓ ↑ net B/s, read/write block B/s, by ID

	logFor    string // container name shown in the log pane ("" = list mode)
	logs      []string
	logScroll int // lines kept off the tail; 0 = follow the stream
	logStream <-chan string
	cancelLog context.CancelFunc

	status  string // transient action feedback
	visible bool   // the active tab; hidden tabs skip table rebuilds
}

// New builds the docker tab.
func New(cfg *config.Config, th *theme.Theme) *Model {
	t := table.New(table.WithFocused(true), table.WithWidth(100), table.WithHeight(18))
	t.SetColumns(columns(100)) // sane defaults until SetSize arrives
	m := &Model{cfg: cfg, th: th, table: t, visible: true}
	m.applyTableStyles()
	return m
}

// SetVisible implements ui.Tab. Hidden tabs keep the latest container
// list but skip the row rebuild; activation catches up.
func (m *Model) SetVisible(visible bool) {
	m.visible = visible
	if visible {
		m.rebuild()
	}
}

// applyTableStyles themes the table: muted header, soft accent fill on
// the focused row.
func (m *Model) applyTableStyles() {
	ts := table.DefaultStyles()
	ts.Header = ts.Header.Foreground(lipgloss.Color(m.th.Palette.Gray))
	ts.Selected = m.th.Styles.Selected.Padding(0, 1)
	m.table.SetStyles(ts)
}

// Title implements ui.Tab.
func (m *Model) Title() string { return ui.Icon("docker", m.cfg.NerdFonts) + " DOCKER" }

// SetTheme implements ui.Tab.
func (m *Model) SetTheme(th *theme.Theme) {
	m.th = th
	m.applyTableStyles()
}

// SetSize implements ui.Tab.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.table.SetWidth(width)
	if m.logFor != "" {
		m.table.SetHeight(0)
	} else {
		m.table.SetHeight(max(3, height-4))
	}
	ui.SetTableColumns(&m.table, columns(width))
	m.rebuild() // resize can change the column shape; rows must follow
}

// Update implements ui.Tab.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case dockerclient.UpdateMsg:
		m.client = msg.Client
		m.err = msg.Err
		m.updateRates(msg.Containers) // rates must keep diffing while hidden
		m.cons = msg.Containers
		if m.visible {
			m.rebuild()
		}
		return nil

	case logLineMsg:
		m.logs = append(m.logs, string(msg))
		if len(m.logs) > dockerLogLines {
			m.logs = m.logs[len(m.logs)-dockerLogLines:]
		}
		return m.waitForLog()

	case logDoneMsg:
		// stream ended (container exited, daemon closed it, or ctx
		// cancelled): stop waiting instead of re-arming on a closed
		// channel, which would busy-loop
		return nil

	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		if m.logFor != "" { // scroll the log buffer
			switch mouse.Button {
			case tea.MouseWheelUp:
				m.logScroll = min(m.logScroll+3, m.scrollbackMax())
			case tea.MouseWheelDown:
				m.logScroll = max(m.logScroll-3, 0)
			}
			return nil
		}
		switch mouse.Button {
		case tea.MouseWheelUp:
			m.table.MoveUp(3)
		case tea.MouseWheelDown:
			m.table.MoveDown(3)
		}
		return nil

	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Button != tea.MouseLeft || m.logFor != "" || m.client == nil {
			return nil
		}
		// window rows: 0 tab bar, 1 head line, 2 table header, 3+ data
		if idx := ui.ClickedRowIndex(m.table, mouse.Y-3); idx >= 0 {
			m.table.SetCursor(idx)
		}
		return nil

	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return nil
}

// scrollbackMax caps log scrolling at the top of the buffer minus one
// screenful so the pane never renders empty.
func (m *Model) scrollbackMax() int {
	return max(0, len(m.logs)-(m.height-3))
}

type logLineMsg string

// logDoneMsg signals that the log stream channel was closed by the
// reader goroutine — the terminal event of waitForLog.
type logDoneMsg struct{}

type actionDoneMsg struct {
	label string
	err   error
}

func (m *Model) key(key tea.KeyPressMsg) tea.Cmd {
	if m.logFor != "" {
		switch key.String() {
		case "esc", "enter":
			m.closeLogs()
		}
		return nil
	}
	if m.client == nil {
		return nil
	}

	switch key.String() {
	case "s":
		return m.action("start", m.client.Start)
	case "t":
		return m.action("stop", m.client.Stop)
	case "r":
		return m.action("restart", m.client.Restart)
	case "enter":
		return m.openLogs()
	default:
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(key)
		return cmd
	}
}

func (m *Model) selected() (dockerclient.Container, bool) {
	cur := m.table.Cursor()
	if cur < 0 || cur >= len(m.cons) {
		return dockerclient.Container{}, false
	}
	return m.cons[cur], true
}

func (m *Model) action(label string, fn func(context.Context, string) error) tea.Cmd {
	ct, ok := m.selected()
	if !ok {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return actionDoneMsg{label: label, err: fn(ctx, ct.ID)}
	}
}

func (m *Model) openLogs() tea.Cmd {
	ct, ok := m.selected()
	if !ok || ct.State != "running" {
		m.status = "logs: container is not running"
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelLog = cancel
	m.logFor = ct.Name
	m.logs = nil
	m.logScroll = 0
	lines := make(chan string, 256)
	m.logStream = lines
	go func() {
		defer close(lines)
		r, tty, err := m.client.Logs(ctx, ct.ID, true)
		if err != nil {
			lines <- "logs: " + err.Error()
			return
		}
		defer func() { _ = r.Close() }()
		pr, pw := io.Pipe()
		go func() {
			err := dockerclient.CopyLogStream(pw, r, tty)
			pw.CloseWithError(err)
		}()
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			select {
			case lines <- sc.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	m.table.SetHeight(0)
	return m.waitForLog()
}

func (m *Model) closeLogs() {
	if m.cancelLog != nil {
		m.cancelLog()
		m.cancelLog = nil
	}
	m.logFor = ""
	m.logs = nil
	m.logScroll = 0
	m.logStream = nil
	m.table.SetHeight(max(3, m.height-4))
}

func (m *Model) waitForLog() tea.Cmd {
	lines := m.logStream
	return func() tea.Msg {
		s, ok := <-lines
		if !ok {
			return logDoneMsg{}
		}
		return logLineMsg(s)
	}
}

// updateRates turns the cumulative per-container counters into rates by
// diffing against the previous poll. A counter that goes backwards
// (container restart) yields zero instead of wrapping huge.
func (m *Model) updateRates(cons []dockerclient.Container) {
	now := time.Now()
	if m.rates == nil {
		m.rates = make(map[string][4]float64)
		m.prev = make(map[string]dockerclient.Container)
	}
	dt := now.Sub(m.prevTime).Seconds()
	next := make(map[string][4]float64, len(cons))
	prev := make(map[string]dockerclient.Container, len(cons))
	for _, c := range cons {
		var r [4]float64
		if p, ok := m.prev[c.ID]; ok && dt > 0 {
			delta := func(cur, old uint64) float64 {
				if cur < old {
					return 0
				}
				return float64(cur-old) / dt
			}
			r = [4]float64{
				delta(c.NetRx, p.NetRx),
				delta(c.NetTx, p.NetTx),
				delta(c.BlkR, p.BlkR),
				delta(c.BlkW, p.BlkW),
			}
		}
		next[c.ID] = r
		prev[c.ID] = c
	}
	m.rates, m.prev, m.prevTime = next, prev, now
}

func (m *Model) rebuild() {
	l := dockerLayoutFor(m.width)
	rows := make([]table.Row, len(m.cons))
	cpuStyle := func(v float64) lipgloss.Style {
		return m.th.Ramps.CPU.StyleAt(min(v, 100) / 100)
	}
	for i, c := range m.cons {
		state := m.th.Styles.Muted.Render(c.State)
		switch c.State {
		case "running":
			state = m.th.Styles.OK.Render(c.State)
		case "exited", "dead":
			state = m.th.Styles.Crit.Render(c.State)
		case "paused":
			state = m.th.Styles.Warn.Render(c.State)
		}
		memPct := m.th.Value(m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit, c.MemPct)
		row := table.Row{
			ui.Trunc(c.Name, 20),
			ui.Trunc(c.Image, l.imageW),
			state,
			cpuStyle(c.CPU).Render(format.CPUPct(c.CPU)),
		}
		if l.bar { // the CPU meter column, wide terminals only
			bar := canvas.GradientBar(10, min(c.CPU, 100)/100, m.th.Ramps.CPU, m.th.Styles.Track)
			if pad := 12 - lipgloss.Width(bar); pad > 0 {
				bar += strings.Repeat(" ", pad)
			}
			row = append(row, bar)
		}
		row = append(row,
			fmt.Sprintf("%10s", format.Bytes(c.Mem)),
			memPct.Render(fmt.Sprintf("%4.0f%%", c.MemPct)),
		)
		if l.net {
			r := m.rates[c.ID]
			row = append(row, fmt.Sprintf("%9s / %-9s", shortRate(r[0]), shortRate(r[1])))
		}
		if l.block {
			r := m.rates[c.ID]
			row = append(row, fmt.Sprintf("%9s / %-9s", shortRate(r[2]), shortRate(r[3])))
		}
		if l.status {
			row = append(row, m.statusCell(c.Status).Render(ui.Trunc(c.Status, 22)))
		}
		rows[i] = row
	}
	m.table.SetRows(rows)
}

// statusCell colors the docker status string by its health verdict
// ("Up 2 hours (healthy)"), when one is present.
func (m *Model) statusCell(status string) lipgloss.Style {
	l := strings.ToLower(status)
	switch {
	case strings.Contains(l, "unhealthy"), strings.Contains(l, "dead"):
		return m.th.Styles.Crit
	case strings.Contains(l, "starting"), strings.Contains(l, "restarting"):
		return m.th.Styles.Warn
	case strings.Contains(l, "healthy"):
		return m.th.Styles.OK
	default:
		return m.th.Styles.Muted
	}
}

// shortRate renders a rate without the "/s" suffix for dense columns.
func shortRate(bps float64) string {
	return strings.TrimSuffix(format.Rate(bps), "/s")
}

// View implements ui.Tab.
func (m *Model) View() string {
	if m.width < 1 {
		return ""
	}
	st := m.th.Styles

	if m.logFor != "" {
		head := st.BorderTitle.Render(" LOGS · "+m.logFor+" ") +
			st.Muted.Render(fmt.Sprintf("  %d lines  ", len(m.logs)))
		if m.logScroll > 0 {
			head += st.Warn.Render(fmt.Sprintf("  ↑%d  ", m.logScroll))
		}
		head += st.HelpKey.Render("esc") + st.HelpText.Render(" back")
		maxLines := max(1, m.height-3)
		start := len(m.logs) - maxLines - m.logScroll
		if start < 0 {
			start = 0
		}
		body := m.logs[start:]
		if len(body) > maxLines {
			body = body[:maxLines]
		}
		styled := make([]string, len(body))
		for i, l := range body {
			if lipgloss.Width(l) > m.width {
				l = ui.Trunc(l, m.width) // rune-safe clip, no soft wrap
			}
			styled[i] = m.severityStyle(l).Render(l)
		}
		return lipgloss.JoinVertical(lipgloss.Left, head, strings.Join(styled, "\n"))
	}

	if m.client == nil {
		msg := st.Warn.Render("Docker daemon not reachable.")
		hint := st.Muted.Render(m.errMsg())
		return lipgloss.JoinVertical(lipgloss.Center, "", msg, hint)
	}

	head := fmt.Sprintf("%d containers", len(m.cons))
	if m.status != "" {
		head += "  " + m.status
	}
	footer := st.HelpText.Render("  ") +
		st.HelpKey.Render("enter") + st.HelpText.Render(" logs  ") +
		st.HelpKey.Render("s") + st.HelpText.Render(" start  ") +
		st.HelpKey.Render("t") + st.HelpText.Render(" stop  ") +
		st.HelpKey.Render("r") + st.HelpText.Render(" restart")
	if len(m.cons) == 0 { // daemon reachable but nothing running on it
		return lipgloss.JoinVertical(lipgloss.Left,
			st.Muted.Render(head),
			"",
			lipgloss.PlaceHorizontal(m.width, lipgloss.Center, st.Muted.Render("□ no containers")),
			footer,
		)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		st.Muted.Render(head),
		m.table.View(),
		footer,
	)
}

// severityStyle colors a log line by the level words it carries — a
// cheap heuristic, but enough to make walls of container output scannable.
func (m *Model) severityStyle(line string) lipgloss.Style {
	l := strings.ToLower(line)
	st := m.th.Styles
	switch {
	case strings.Contains(l, "fatal"), strings.Contains(l, "panic"),
		strings.Contains(l, "error"), strings.Contains(l, "err:"):
		return st.Crit
	case strings.Contains(l, "warn"):
		return st.Warn
	case strings.Contains(l, "debug"), strings.Contains(l, "trace"):
		return st.Muted
	}
	return lipgloss.NewStyle()
}

func (m *Model) errMsg() string {
	if m.err != nil {
		return m.err.Error()
	}
	return "start Docker Desktop or the daemon, then come back — wrongtop retries automatically."
}

// dockerLayout decides which container columns fit the terminal. The
// full table needs ~160 columns, so extras drop in priority order
// (BLOCK, then STATUS, then NET) and IMAGE absorbs the remaining slack.
type dockerLayout struct {
	net, block, status, bar bool
	imageW                  int
}

func dockerLayoutFor(width int) dockerLayout {
	l := dockerLayout{imageW: 12}
	switch {
	case width >= 146:
		l.net, l.block, l.status, l.bar = true, true, true, true
		l.imageW = max(12, width-142)
	case width >= 118:
		l.net, l.status, l.bar = true, true, true
		l.imageW = max(12, width-119)
	case width >= 94:
		l.net, l.bar = true, true
		l.imageW = max(12, width-95)
	default:
		l.imageW = max(12, width-62)
	}
	return l
}

func columns(width int) []table.Column {
	l := dockerLayoutFor(width)
	cols := []table.Column{
		{Title: "NAME", Width: 20},
		{Title: "IMAGE", Width: l.imageW},
		{Title: "STATE", Width: 9},
		{Title: "CPU%", Width: 6},
	}
	if l.bar {
		cols = append(cols, table.Column{Title: "ACTIVITY", Width: 12})
	}
	cols = append(cols,
		table.Column{Title: "MEM", Width: 10},
		table.Column{Title: "MEM%", Width: 5},
	)
	if l.net {
		cols = append(cols, table.Column{Title: "NET ↓/↑", Width: 21})
	}
	if l.block {
		cols = append(cols, table.Column{Title: "BLOCK ↓/↑", Width: 21})
	}
	if l.status {
		cols = append(cols, table.Column{Title: "STATUS", Width: 22})
	}
	return cols
}
