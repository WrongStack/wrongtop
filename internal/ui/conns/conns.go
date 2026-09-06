// Package conns renders the connections tab: the live TCP table, live
// traffic first, with per-state coloring and best-effort process names.
package conns

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/theme"
	"github.com/wrongstack/wrongtop/internal/ui"
)

// Model is the connections tab.
type Model struct {
	cfg *config.Config
	th  *theme.Theme

	width, height int
	table         table.Model
	conns         []collector.Conn
	live          bool
	anyPID        bool
	visible       bool             // the active tab; hidden tabs skip rebuilds
	procs         []collector.Proc // latest snapshot for pid-name lookups
	pidName       map[int32]string // from the process snapshot
}

// New builds the connections tab.
func New(cfg *config.Config, th *theme.Theme) *Model {
	t := table.New(table.WithFocused(true), table.WithWidth(100), table.WithHeight(20))
	t.SetColumns(columns(100, true)) // sane defaults until SetSize arrives
	m := &Model{cfg: cfg, th: th, table: t, pidName: make(map[int32]string), visible: true}
	m.applyTableStyles()
	return m
}

// SetVisible implements ui.Tab. Hidden tabs keep the latest tables but
// skip the rebuild; activation catches up.
func (m *Model) SetVisible(visible bool) {
	m.visible = visible
	if visible {
		m.syncPIDNames(m.procs)
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
func (m *Model) Title() string { return ui.Icon("conn", m.cfg.NerdFonts) + " CONNECTIONS" }

// SetTheme implements ui.Tab.
func (m *Model) SetTheme(th *theme.Theme) {
	m.th = th
	m.applyTableStyles()
}

// SetSize implements ui.Tab.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.table.SetWidth(width)
	m.table.SetHeight(max(3, height-2)) // head line
	ui.SetTableColumns(&m.table, columns(width, m.anyPID))
	m.rebuild() // keep rows in step with the column shape
}

// Update implements ui.Tab.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case collector.SnapshotMsg:
		m.conns = msg.Snap.Conns
		m.live = true
		m.procs = msg.Snap.Procs
		if m.visible { // hidden: skip the rebuild, catch up on activate
			m.syncPIDNames(m.procs)
			m.rebuild()
		}
		return nil

	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		switch mouse.Button {
		case tea.MouseWheelUp:
			m.table.MoveUp(3)
		case tea.MouseWheelDown:
			m.table.MoveDown(3)
		}
		return nil

	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Button != tea.MouseLeft {
			return nil
		}
		// window rows: 0 tab bar, 1 head line, 2 table header, 3+ data
		if idx := ui.ClickedRowIndex(m.table, mouse.Y-3); idx >= 0 {
			m.table.SetCursor(idx)
		}
		return nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "up":
			m.table.MoveUp(1)
		case "down":
			m.table.MoveDown(1)
		}
		return nil
	}
	return nil
}

// syncPIDNames keeps a pid → process name map from the latest process
// snapshot; the process column appears only when the platform resolves
// connection PIDs at all.
func (m *Model) syncPIDNames(procs []collector.Proc) {
	next := make(map[int32]string, len(procs))
	for _, p := range procs {
		next[p.PID] = p.Name
	}
	m.pidName = next
	any := false
	for _, c := range m.conns {
		if c.PID > 0 {
			any = true
			break
		}
	}
	if any != m.anyPID {
		m.anyPID = any
		// the table re-renders on both calls and indexes row cells by
		// column count, so the rows must be empty across the transition
		m.table.SetRows(nil)
		m.table.SetColumns(columns(m.width, any))
		m.rebuild()
	}
}

// connLayout is the shared column plan; rebuild() and columns() must
// consult the same numbers or rows and headers disagree.
type connLayout struct {
	localW, remoteW int
	pid             bool
}

func layoutFor(width int, anyPID bool) connLayout {
	l := connLayout{localW: 24, remoteW: 28, pid: anyPID}
	switch {
	case width < 84:
		l.localW, l.remoteW = 20, 20
	case width < 108:
		l.localW, l.remoteW = 22, 24
	}
	return l
}

func (m *Model) rebuild() {
	l := layoutFor(m.width, m.anyPID)
	rows := make([]table.Row, len(m.conns))
	for i, c := range m.conns {
		row := table.Row{
			fmt.Sprintf("%-*s", l.localW, ui.Trunc(c.Local, l.localW)),
			fmt.Sprintf("%-*s", l.remoteW, ui.Trunc(remoteLabel(c.Remote), l.remoteW)),
			m.stateStyle(c.State).Render(c.State),
		}
		if l.pid {
			name := "—"
			if c.PID > 0 {
				if n, ok := m.pidName[c.PID]; ok && n != "" {
					name = ui.Trunc(n, 24)
				} else {
					name = fmt.Sprintf("%d", c.PID)
				}
			}
			row = append(row, fmt.Sprintf("%7d", c.PID), name)
		}
		rows[i] = row
	}
	m.table.SetRows(rows)
}

// stateStyle colors the TCP state: live traffic green, listeners cyan,
// decomposing states muted, trouble states yellow.
func (m *Model) stateStyle(state string) lipgloss.Style {
	st := m.th.Styles
	switch state {
	case "ESTABLISHED":
		return st.OK
	case "LISTEN":
		return st.HelpKey
	case "TIME_WAIT", "CLOSE":
		return st.Muted
	case "CLOSE_WAIT", "LAST_ACK", "SYN_SENT", "SYN_RECV":
		return st.Warn
	default:
		return st.Muted
	}
}

// View implements ui.Tab.
func (m *Model) View() string {
	if m.width < 1 {
		return ""
	}
	st := m.th.Styles
	established, listen := 0, 0
	for _, c := range m.conns {
		switch c.State {
		case "ESTABLISHED":
			established++
		case "LISTEN":
			listen++
		}
	}
	head := st.FG.Render(fmt.Sprintf("%d conns", len(m.conns))) +
		st.Muted.Render("  ·  ") + st.OK.Render(fmt.Sprintf("%d established", established)) +
		st.Muted.Render("  ·  ") + st.HelpKey.Render(fmt.Sprintf("%d listen", listen))
	if !m.anyPID {
		head += st.Muted.Render("  ·  process ids unavailable on this platform")
	}
	if !m.live {
		head += st.Muted.Render("  ·  waiting for samples…")
	}

	body := m.table.View()
	if len(m.conns) == 0 && m.live {
		body = lipgloss.PlaceHorizontal(m.width, lipgloss.Center,
			st.Muted.Render("□ no TCP connections"))
	}
	return lipgloss.JoinVertical(lipgloss.Left, head, body)
}

// remoteLabel renders the far end of a connection; listeners get a dash.
func remoteLabel(remote string) string {
	if remote == "" || strings.HasSuffix(remote, ":*") {
		return "*"
	}
	return remote
}

// columns builds the header from the shared layout plan.
func columns(width int, anyPID bool) []table.Column {
	l := layoutFor(width, anyPID)
	cols := []table.Column{
		{Title: "LOCAL", Width: l.localW},
		{Title: "REMOTE", Width: l.remoteW},
		{Title: "STATE", Width: 12},
	}
	if l.pid {
		cols = append(cols,
			table.Column{Title: "PID", Width: 7},
			table.Column{Title: "PROCESS", Width: max(12, width-l.localW-l.remoteW-12-7)})
	}
	return cols
}
