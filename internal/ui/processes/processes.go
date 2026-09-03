// Package processes renders the process table tab: sortable, filterable,
// with kill actions behind a confirmation prompt.
package processes

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ersinkoc/wrongtop/internal/collector"
	"github.com/ersinkoc/wrongtop/internal/config"
	"github.com/ersinkoc/wrongtop/internal/format"
	"github.com/ersinkoc/wrongtop/internal/procs"
	"github.com/ersinkoc/wrongtop/internal/theme"
	"github.com/ersinkoc/wrongtop/internal/ui"
)

// Model is the processes tab.
type Model struct {
	cfg *config.Config
	th  *theme.Theme

	width, height int

	table table.Model
	input textinput.Model

	procs   []collector.Proc // latest snapshot, unfiltered
	visible []collector.Proc // rows currently shown, in order
	sort    procs.SortKey
	desc    bool
	filter  string
	editing bool // filter input active

	confirm *killConfirm // non-nil while the kill prompt is open
	status  string       // transient action feedback
}

type killConfirm struct {
	pid   int32
	name  string
	force bool
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
	return &Model{
		cfg:   cfg,
		th:    th,
		table: t,
		input: in,
		desc:  true,
	}
}

// Title implements ui.Tab.
func (m *Model) Title() string { return "PROCESSES" }

// SetSize implements ui.Tab.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.table.SetWidth(tableWidth(width))
	m.table.SetHeight(max(3, height-4)) // info line + footer
	m.table.SetColumns(columns(width))
}

// Update implements ui.Tab.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case collector.SnapshotMsg:
		m.procs = msg.Snap.Procs
		m.rebuild()
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
	case "y":
		m.confirm = nil
		var err error
		if c.force {
			err = procs.ForceKill(c.pid)
		} else {
			err = procs.Kill(c.pid)
		}
		if err != nil {
			m.status = m.th.Styles.Crit.Render(err.Error())
		} else {
			m.status = m.th.Styles.OK.Render(fmt.Sprintf("signal sent to %d", c.pid))
		}
	case "f", m.forceKey():
		m.confirm.force = true
	case "n", "esc":
		m.confirm = nil
	}
	return nil
}

func (m *Model) openConfirm(force bool) tea.Cmd {
	if len(m.visible) == 0 {
		return nil
	}
	p := m.visible[m.table.Cursor()]
	m.confirm = &killConfirm{pid: p.PID, name: p.Name, force: force}
	return nil
}

// rebuild re-applies filter + sort, rebuilds rows and restores the
// cursor on the previously selected PID.
func (m *Model) rebuild() {
	selected := int32(0)
	if cur := m.table.Cursor(); cur >= 0 && cur < len(m.visible) {
		selected = m.visible[cur].PID
	}

	m.visible = procs.Filter(append([]collector.Proc(nil), m.procs...), m.filter)
	procs.Sort(m.visible, m.sort, m.desc)

	rows := make([]table.Row, len(m.visible))
	for i, p := range m.visible {
		rows[i] = m.row(p)
	}
	m.table.SetRows(rows)

	if selected != 0 {
		for i, p := range m.visible {
			if p.PID == selected {
				m.table.SetCursor(i)
				break
			}
		}
	}
}

func (m *Model) row(p collector.Proc) table.Row {
	cpu := m.th.Value(m.cfg.Thresholds.CPUWarn, m.cfg.Thresholds.CPUCrit, p.CPU)
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
	return table.Row{
		fmt.Sprintf("%7d", p.PID),
		p.Name,
		cpu.Render(fmt.Sprintf("%5.1f", p.CPU)),
		mem.Render(fmt.Sprintf("%5.1f", p.Mem)),
		fmt.Sprintf("%6s", format.Bytes(p.RSS)),
		trunc(p.User, 12),
		fmt.Sprintf("%3d", p.Threads),
		state.Render(p.State),
	}
}

// killKey returns the configured terminate key, lowercased; its uppercase
// variant triggers a force kill.
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
		parts = append(parts, "", lipgloss.PlaceHorizontal(m.width, lipgloss.Center, m.confirmView()))
	}
	parts = append(parts, m.footerView())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *Model) infoView() string {
	sortArrow := "↓"
	if !m.desc {
		sortArrow = "↑"
	}
	info := m.th.Styles.Muted.Render(fmt.Sprintf("%d procs · sort: ", len(m.visible))) +
		m.th.Styles.HelpKey.Render(m.sort.String()+sortArrow)
	if m.filter != "" {
		info += m.th.Styles.Muted.Render(" · filter: ") + m.th.Styles.Warn.Render(m.filter)
	}
	if m.status != "" {
		info += "  " + m.status
	}
	return info
}

func (m *Model) footerView() string {
	if m.confirm != nil {
		return m.th.Styles.HelpText.Render("  ") +
			m.th.Styles.HelpKey.Render("y") + m.th.Styles.HelpText.Render(" term  ") +
			m.th.Styles.HelpKey.Render(m.forceKey()) + m.th.Styles.HelpText.Render(" kill -9  ") +
			m.th.Styles.HelpKey.Render("esc") + m.th.Styles.HelpText.Render(" cancel")
	}
	if m.editing {
		return "  " + m.input.View()
	}
	return m.th.Styles.HelpText.Render("  ") +
		m.th.Styles.HelpKey.Render(m.cfg.Keys.Filter) + m.th.Styles.HelpText.Render(" filter  ") +
		m.th.Styles.HelpKey.Render("s") + m.th.Styles.HelpText.Render(" sort  ") +
		m.th.Styles.HelpKey.Render("S") + m.th.Styles.HelpText.Render(" reverse  ") +
		m.th.Styles.HelpKey.Render(m.killKey()) + m.th.Styles.HelpText.Render(" term  ") +
		m.th.Styles.HelpKey.Render(m.forceKey()) + m.th.Styles.HelpText.Render(" kill")
}

func (m *Model) confirmView() string {
	action := "terminate"
	if m.confirm.force {
		action = "kill -9"
	}
	body := m.th.Styles.Muted.Render(
		fmt.Sprintf("%s %d (%s)?  ", action, m.confirm.pid, trunc(m.confirm.name, 24)))
	return ui.Box(lipgloss.RoundedBorder(), m.th.Styles.Border, m.th.Styles.BorderChar,
		m.th.Styles.BorderTitle, "KILL", body)
}

// columns builds the table header; NAME absorbs the remaining width.
func columns(width int) []table.Column {
	fixed := 7 + 6 + 6 + 7 + 12 + 4 + 2
	nameW := max(16, width-fixed)
	return []table.Column{
		{Title: "PID", Width: 7},
		{Title: "NAME", Width: nameW},
		{Title: "CPU%", Width: 6},
		{Title: "MEM%", Width: 6},
		{Title: "RSS", Width: 7},
		{Title: "USER", Width: 12},
		{Title: "THR", Width: 4},
		{Title: "S", Width: 2},
	}
}

func tableWidth(width int) int { return max(40, width) }

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
