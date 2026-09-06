// Package processes renders the process table tab: sortable, filterable,
// flat or tree view, with a signal picker behind a confirmation prompt.
package processes

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/format"
	"github.com/wrongstack/wrongtop/internal/procs"
	"github.com/wrongstack/wrongtop/internal/theme"
	"github.com/wrongstack/wrongtop/internal/ui"
	"github.com/wrongstack/wrongtop/internal/ui/canvas"
)

// Model is the processes tab.
type Model struct {
	cfg *config.Config
	th  *theme.Theme

	width, height int

	table table.Model
	input textinput.Model

	procs     []collector.Proc // latest snapshot, unfiltered
	rows      []procs.TreeNode // rows currently shown, in display order
	sort      procs.SortKey
	desc      bool
	filter    string
	editing   bool // filter input active
	tree      bool // parent-child view
	collapsed map[int32]bool
	visible   bool // the active tab; hidden tabs skip table rebuilds

	confirm *killConfirm // non-nil while the signal prompt is open
	detail  *procDetail  // non-nil while the detail box is shown
	status  string       // transient action feedback
}

// procDetail backs the per-process detail box; the full command line is
// fetched on demand, off the collection loop.
type procDetail struct {
	proc    collector.Proc
	cmdline string
}

// cmdlineMsg delivers an on-demand command line lookup.
type cmdlineMsg struct {
	pid  int32
	text string
}

type killConfirm struct {
	pid  int32
	name string
	sigs []procs.Signal // signals offerable on this platform
	sig  int            // index into sigs
}

// New builds the processes tab.
func New(cfg *config.Config, th *theme.Theme) *Model {
	in := textinput.New()
	in.Prompt = "filter> "
	in.CharLimit = 64
	cols := columns(100)
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithWidth(tableWidth(100)),
		table.WithHeight(20),
	)
	m := &Model{
		cfg:       cfg,
		th:        th,
		table:     t,
		input:     in,
		desc:      true,
		collapsed: make(map[int32]bool),
		visible:   true, // the app dims non-active tabs right after New
	}
	m.applyTableStyles()
	return m
}

// SetVisible implements ui.Tab. Hidden tabs keep the latest snapshot
// but skip the 700-row rebuild; activation catches up.
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
func (m *Model) Title() string { return ui.Icon("proc", m.cfg.NerdFonts) + " PROCESSES" }

// SetTheme implements ui.Tab.
func (m *Model) SetTheme(th *theme.Theme) {
	m.th = th
	m.applyTableStyles()
}

// SetSize implements ui.Tab.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.table.SetWidth(tableWidth(width))
	m.table.SetHeight(max(3, height-4)) // info line + footer
	ui.SetTableColumns(&m.table, columns(width))
	m.rebuild() // resize can change the column shape; rows must follow
}

// Update implements ui.Tab.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case collector.SnapshotMsg:
		m.procs = msg.Snap.Procs
		if m.visible { // hidden: skip the heavy rebuild, catch up on activate
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
		if mouse.Button != tea.MouseLeft || m.confirm != nil || m.editing {
			return nil
		}
		// window rows: 0 tab bar, 1 info line, 2 table header, 3+ data
		if idx := ui.ClickedRowIndex(m.table, mouse.Y-3); idx >= 0 {
			m.table.SetCursor(idx)
		}
		return nil

	case cmdlineMsg:
		if m.detail != nil && m.detail.proc.PID == msg.pid {
			m.detail.cmdline = msg.text
		}
		return nil

	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return nil
}

func (m *Model) key(key tea.KeyPressMsg) tea.Cmd {
	if m.confirm != nil {
		return m.confirmKey(key)
	}
	if m.editing {
		return m.filterKey(key)
	}
	if m.detail != nil && key.String() == "esc" {
		m.detail = nil
		return nil
	}

	switch key.String() {
	case m.cfg.Keys.Filter:
		m.editing = true
		return m.input.Focus()
	case "s":
		m.sort = m.sort.Next()
		m.rebuild()
	case "S":
		m.desc = !m.desc
		m.rebuild()
	case "t":
		m.tree = !m.tree
		m.rebuild()
	case "enter":
		return m.openDetail()
	case "left", "right":
		m.expandKey(key.String() == "right")
	case m.killKey():
		return m.openConfirm(false)
	case m.forceKey():
		return m.openConfirm(true)
	default:
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(key)
		return cmd
	}
	return nil
}

// openDetail shows the selected process's details and fetches its full
// command line in the background.
func (m *Model) openDetail() tea.Cmd {
	cur := m.table.Cursor()
	if cur < 0 || cur >= len(m.rows) {
		return nil
	}
	p := m.rows[cur].Proc
	m.detail = &procDetail{proc: p}
	if m.cfg.ReadOnly {
		return nil // a remote view cannot look up local command lines
	}
	pid := p.PID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return cmdlineMsg{pid: pid, text: collector.Cmdline(ctx, pid)}
	}
}

// expandKey collapses (right=false) or expands (right=true) the selected
// process's subtree in tree mode.
func (m *Model) expandKey(expand bool) {
	if !m.tree {
		return
	}
	cur := m.table.Cursor()
	if cur < 0 || cur >= len(m.rows) || !m.rows[cur].Children {
		return
	}
	pid := m.rows[cur].Proc.PID
	if expand {
		delete(m.collapsed, pid)
	} else {
		m.collapsed[pid] = true
	}
	m.rebuild()
}

func (m *Model) filterKey(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "enter":
		m.editing = false
		m.input.Blur()
		m.filter = m.input.Value()
		m.rebuild()
		return nil
	case "esc":
		m.editing = false
		m.input.Blur()
		m.input.SetValue("")
		m.filter = ""
		m.rebuild()
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(key)
	if v := m.input.Value(); v != m.filter {
		m.filter = v
		m.rebuild()
	}
	return cmd
}

func (m *Model) confirmKey(key tea.KeyPressMsg) tea.Cmd {
	c := m.confirm
	switch key.String() {
	case "left":
		c.sig = (c.sig - 1 + len(c.sigs)) % len(c.sigs)
	case "right":
		c.sig = (c.sig + 1) % len(c.sigs)
	case "y":
		m.confirm = nil
		sig := c.sigs[c.sig]
		if err := procs.SendSignal(c.pid, sig.Name); err != nil {
			m.status = m.th.Styles.Crit.Render(err.Error())
		} else {
			m.status = m.th.Styles.OK.Render(fmt.Sprintf("SIG%s sent to %d", sig.Name, c.pid))
		}
	case "f", m.forceKey():
		if idx := killSignalIndex(c.sigs); idx >= 0 {
			c.sig = idx
		}
	case "n", "esc":
		m.confirm = nil
	}
	return nil
}

// killSignalIndex finds KILL in the offerable signals; -1 when absent.
func killSignalIndex(sigs []procs.Signal) int {
	for i, s := range sigs {
		if s.Name == "KILL" {
			return i
		}
	}
	return -1
}

func (m *Model) openConfirm(force bool) tea.Cmd {
	if m.cfg.ReadOnly {
		m.status = m.th.Styles.Warn.Render("read-only view — signaling disabled")
		return nil
	}
	if len(m.rows) == 0 {
		return nil
	}
	cur := m.table.Cursor()
	if cur < 0 || cur >= len(m.rows) {
		return nil
	}
	p := m.rows[cur].Proc
	c := &killConfirm{pid: p.PID, name: p.Name, sigs: procs.AvailableSignals()}
	if force {
		c.sig = killSignalIndex(c.sigs)
		if c.sig < 0 {
			c.sig = 0
		}
	}
	m.confirm = c
	return nil
}

// rebuild re-applies filter + sort (or the tree builder), rebuilds rows
// and restores the cursor on the previously selected PID.
func (m *Model) rebuild() {
	selected := int32(0)
	if cur := m.table.Cursor(); cur >= 0 && cur < len(m.rows) {
		selected = m.rows[cur].Proc.PID
	}

	filtered := procs.Filter(append([]collector.Proc(nil), m.procs...), m.filter)
	if m.tree {
		m.rows = procs.BuildTree(filtered, m.sort, m.desc, m.collapsed)
	} else {
		procs.Sort(filtered, m.sort, m.desc)
		m.rows = make([]procs.TreeNode, len(filtered))
		for i, p := range filtered {
			m.rows[i] = procs.TreeNode{Proc: p}
		}
	}

	rows := make([]table.Row, len(m.rows))
	for i, node := range m.rows {
		rows[i] = m.row(node)
	}
	m.table.SetRows(rows)

	if selected != 0 {
		found := false
		for i, node := range m.rows {
			if node.Proc.PID == selected {
				m.table.SetCursor(i)
				ui.EnsureRowVisible(&m.table, i)
				found = true
				break
			}
		}
		if !found && m.detail != nil && m.detail.proc.PID == selected {
			m.detail = nil // the selected process exited
		}
	}
	// a cleared or shrunken table can leave the cursor out of range;
	// every consumer assumes a valid index
	if cur := m.table.Cursor(); cur < 0 || cur >= len(m.rows) {
		if len(m.rows) > 0 {
			m.table.SetCursor(0)
		}
	}
}

// rowName renders the NAME cell; tree rows get depth guides and a
// collapse marker.
func (m *Model) rowName(node procs.TreeNode) string {
	if !m.tree {
		return node.Proc.Name
	}
	var b strings.Builder
	for d := 0; d < node.Depth; d++ {
		b.WriteString("│ ")
	}
	switch {
	case node.Children && m.collapsed[node.Proc.PID]:
		b.WriteString("▸ ")
	case node.Children:
		b.WriteString("▾ ")
	case node.Depth > 0:
		b.WriteString("└ ")
	}
	return b.String() + node.Proc.Name
}

func (m *Model) row(node procs.TreeNode) table.Row {
	p := node.Proc
	// CPU colored along the value ramp, MEM by thresholds
	cpuStyle := m.th.Ramps.CPU.StyleAt(min(p.CPU, 100) / 100)
	mem := m.th.Value(m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit, p.Mem)
	state := m.th.Styles.Muted
	switch p.State {
	case "R":
		state = m.th.Styles.OK
	case "Z":
		state = m.th.Styles.Crit
	case "T":
		state = m.th.Styles.Warn
	}
	row := table.Row{
		fmt.Sprintf("%7d", p.PID),
		m.rowName(node),
		cpuStyle.Render(format.CPUPct(p.CPU)),
	}
	if m.width >= activityMinWidth { // the CPU meter column, wide terminals only
		bar := canvas.GradientBar(10, min(p.CPU, 100)/100, m.th.Ramps.CPU, m.th.Styles.Track)
		if pad := activityWidth - lipgloss.Width(bar); pad > 0 {
			bar += strings.Repeat(" ", pad)
		}
		row = append(row, bar)
	}
	row = append(row,
		mem.Render(fmt.Sprintf("%5.1f", p.Mem)),
		fmt.Sprintf("%6s", format.Bytes(p.RSS)),
		ui.Trunc(p.User, 12),
		fmt.Sprintf("%3d", p.Threads),
		state.Render(p.State),
	)
	return row
}

// activityMinWidth/activityWidth gate the per-process CPU meter column.
const (
	activityMinWidth = 112
	activityWidth    = 12
)

// killKey returns the configured terminate key, lowercased; its uppercase
// variant jumps the picker straight to SIGKILL.
func (m *Model) killKey() string { return strings.ToLower(m.cfg.Keys.Kill) }

// forceKey returns the force-kill variant of the terminate key.
func (m *Model) forceKey() string { return strings.ToUpper(m.killKey()) }

// View implements ui.Tab.
func (m *Model) View() string {
	if m.width < 1 {
		return ""
	}
	parts := []string{m.infoView(), m.table.View()}
	if m.confirm != nil {
		parts = append(parts, "", lipgloss.PlaceHorizontal(m.width, lipgloss.Center,
			ui.Shadow(m.confirmView(), m.th.Styles.Muted)))
	} else if m.detail != nil {
		parts = append(parts, "", lipgloss.PlaceHorizontal(m.width, lipgloss.Center,
			ui.Shadow(m.detailView(), m.th.Styles.Muted)))
	}
	parts = append(parts, m.footerView())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// detailView renders the selected process's details: identity, resource
// figures and the full command line, hard-wrapped to the box width so
// long commands stay readable instead of being clipped.
func (m *Model) detailView() string {
	p := m.detail.proc
	st := m.th.Styles
	cmd := m.detail.cmdline
	if cmd == "" {
		cmd = p.Name
	}
	if w := m.width - 12; w > 8 {
		cmd = lipgloss.NewStyle().Width(w).Render(cmd)
	}
	kv := func(key, val string) string {
		return st.Muted.Render(fmt.Sprintf("%-9s", key)) + val
	}
	body := strings.Join([]string{
		kv("PID", fmt.Sprintf("%d  (ppid %d)", p.PID, p.PPID)),
		kv("USER", p.User),
		kv("STATE", p.State),
		kv("THREADS", fmt.Sprintf("%d · nice %d", p.Threads, p.Nice)),
		kv("CPU/MEM", fmt.Sprintf("%.1f%% · %.1f%%", p.CPU, p.Mem)),
		kv("RSS", format.Bytes(p.RSS)),
		"",
		cmd,
	}, "\n")
	return ui.Box(ui.BorderFor(m.cfg.Border), st.Border, st.BorderChar, st.BorderTitle,
		"PROCESS", "", body)
}

func (m *Model) infoView() string {
	sortArrow := "↓"
	if !m.desc {
		sortArrow = "↑"
	}
	info := m.th.Styles.Muted.Render(fmt.Sprintf("%d procs · sort: ", len(m.rows))) +
		m.th.Styles.HelpKey.Render(m.sort.String()+sortArrow)
	if m.tree {
		info += m.th.Styles.Muted.Render(" · ") + m.th.Styles.HelpKey.Render("tree")
	}
	if m.filter != "" {
		info += m.th.Styles.Muted.Render(" · filter: ") + m.th.Styles.Warn.Render(m.filter)
	}
	// glances-style session health: flag anomalies in the info line
	var zombies, stopped int
	for _, p := range m.procs {
		switch p.State {
		case "Z":
			zombies++
		case "T":
			stopped++
		}
	}
	if zombies > 0 {
		info += m.th.Styles.Crit.Render(fmt.Sprintf(" · %d zombie", zombies))
	}
	if stopped > 0 {
		info += m.th.Styles.Warn.Render(fmt.Sprintf(" · %d stopped", stopped))
	}
	if m.status != "" {
		info += "  " + m.status
	}
	return info
}

func (m *Model) footerView() string {
	if m.confirm != nil {
		return m.th.Styles.HelpText.Render("  ") +
			m.th.Styles.HelpKey.Render("←→") + m.th.Styles.HelpText.Render(" signal  ") +
			m.th.Styles.HelpKey.Render("y") + m.th.Styles.HelpText.Render(" send  ") +
			m.th.Styles.HelpKey.Render(m.forceKey()) + m.th.Styles.HelpText.Render(" kill  ") +
			m.th.Styles.HelpKey.Render("esc") + m.th.Styles.HelpText.Render(" cancel")
	}
	if m.editing {
		return "  " + m.input.View()
	}
	return m.th.Styles.HelpText.Render("  ") +
		m.th.Styles.HelpKey.Render(m.cfg.Keys.Filter) + m.th.Styles.HelpText.Render(" filter  ") +
		m.th.Styles.HelpKey.Render("s") + m.th.Styles.HelpText.Render(" sort  ") +
		m.th.Styles.HelpKey.Render("S") + m.th.Styles.HelpText.Render(" reverse  ") +
		m.th.Styles.HelpKey.Render("t") + m.th.Styles.HelpText.Render(" tree  ") +
		m.th.Styles.HelpKey.Render(m.killKey()) + m.th.Styles.HelpText.Render(" signal")
}

func (m *Model) confirmView() string {
	sig := m.confirm.sigs[m.confirm.sig]
	body := m.th.Styles.Muted.Render(
		fmt.Sprintf("SIG%s (%s) → %d (%s)?  ", sig.Name, sig.Desc,
			m.confirm.pid, ui.Trunc(m.confirm.name, 24)))
	return ui.Box(ui.BorderFor(m.cfg.Border), m.th.Styles.Border, m.th.Styles.BorderChar,
		m.th.Styles.BorderTitle, "SIGNAL", "", body)
}

// columns builds the table header; NAME absorbs the remaining width. The
// ACTIVITY meter column appears only on wide terminals — rows() and this
// must consult the same predicate.
func columns(width int) []table.Column {
	fixed := 7 + 6 + 6 + 7 + 12 + 4 + 2
	if width >= activityMinWidth {
		fixed += activityWidth
	}
	nameW := max(16, width-fixed)
	cols := []table.Column{
		{Title: "PID", Width: 7},
		{Title: "NAME", Width: nameW},
		{Title: "CPU%", Width: 6},
	}
	if width >= activityMinWidth {
		cols = append(cols, table.Column{Title: "ACTIVITY", Width: activityWidth})
	}
	return append(cols,
		table.Column{Title: "MEM%", Width: 6},
		table.Column{Title: "RSS", Width: 7},
		table.Column{Title: "USER", Width: 12},
		table.Column{Title: "THR", Width: 4},
		table.Column{Title: "S", Width: 2},
	)
}

func tableWidth(width int) int { return max(40, width) }
