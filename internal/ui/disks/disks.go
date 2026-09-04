// Package disks renders the disks tab: filesystem usage and per-device
// I/O rates.
package disks

import (
	"fmt"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ersinkoc/wrongtop/internal/collector"
	"github.com/ersinkoc/wrongtop/internal/config"
	"github.com/ersinkoc/wrongtop/internal/format"
	"github.com/ersinkoc/wrongtop/internal/theme"
	"github.com/ersinkoc/wrongtop/internal/ui"
	"github.com/ersinkoc/wrongtop/internal/ui/canvas"
)

// Model is the disks tab.
type Model struct {
	cfg *config.Config
	th  *theme.Theme

	width, height int
	table         table.Model
	io            table.Model
	focus         int // 0 = filesystem table, 1 = io table

	disks   []collector.Disk
	diskIOs []collector.DiskIO
	live    bool
}

// New builds the disks tab.
func New(cfg *config.Config, th *theme.Theme) *Model {
	t := table.New(table.WithFocused(true), table.WithWidth(100), table.WithHeight(12))
	io := table.New(table.WithFocused(false), table.WithWidth(100), table.WithHeight(8))
	t.SetColumns(usageColumns(100)) // sane defaults until SetSize arrives
	io.SetColumns(ioColumns(100))
	return &Model{cfg: cfg, th: th, table: t, io: io}
}

// Title implements ui.Tab.
func (m *Model) Title() string { return "DISKS" }

// SetTheme implements ui.Tab.
func (m *Model) SetTheme(th *theme.Theme) { m.th = th }

// SetSize implements ui.Tab.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	h := max(3, (height-8)/2)
	m.table.SetWidth(width)
	m.table.SetHeight(h)
	m.io.SetWidth(width)
	m.io.SetHeight(max(2, height-8-h))
	m.table.SetColumns(usageColumns(width))
	m.io.SetColumns(ioColumns(width))
}

// Update implements ui.Tab.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case collector.SnapshotMsg:
		m.disks = msg.Snap.Disks
		m.diskIOs = msg.Snap.DiskIOs
		m.live = true
		m.rebuild()
		return nil

	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		t := &m.table
		if mouse.Y-6-m.table.Height() >= 0 { // pointer is over the io table
			t = &m.io
		}
		switch mouse.Button {
		case tea.MouseWheelUp:
			t.MoveUp(3)
		case tea.MouseWheelDown:
			t.MoveDown(3)
		}
		return nil

	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Button != tea.MouseLeft {
			return nil
		}
		// window rows: 0 tab bar, 1 fs header, 2 table header, 3+ fs data,
		// then a blank + io header + io table header before io data
		if row := mouse.Y - 3; row >= 0 && row < m.table.Height() {
			if idx := ui.ClickedRowIndex(m.table, row); idx >= 0 {
				m.table.SetCursor(idx)
				m.focus = 0
			}
			return nil
		}
		if row := mouse.Y - 6 - m.table.Height(); row >= 0 {
			if idx := ui.ClickedRowIndex(m.io, row); idx >= 0 {
				m.io.SetCursor(idx)
				m.focus = 1
			}
		}
		return nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "up":
			m.focusedTable().MoveUp(1)
		case "down":
			m.focusedTable().MoveDown(1)
		case "left", "right":
			m.focus = 1 - m.focus
		}
		return nil
	}
	return nil
}

// focusedTable returns the table currently under keyboard control.
func (m *Model) focusedTable() *table.Model {
	if m.focus == 1 {
		return &m.io
	}
	return &m.table
}

func (m *Model) rebuild() {
	rows := make([]table.Row, len(m.disks))
	for i, d := range m.disks {
		style := m.th.Value(m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit, d.Percent)
		rows[i] = table.Row{
			trunc(d.Device, 16),
			trunc(d.Mountpoint, 26),
			trunc(d.FSType, 8),
			style.Render(canvas.Bar(14, d.Percent/100, style, m.th.Styles.Muted)),
			fmt.Sprintf("%7s", format.Bytes(d.Used)),
			fmt.Sprintf("%7s", format.Bytes(d.Total)),
			style.Render(fmt.Sprintf("%4.0f%%", d.Percent)),
		}
	}
	m.table.SetRows(rows)

	rows = make([]table.Row, len(m.diskIOs))
	for i, d := range m.diskIOs {
		rows[i] = table.Row{
			trunc(d.Name, 16),
			fmt.Sprintf("%9s", format.Rate(d.ReadBytes)),
			fmt.Sprintf("%9s", format.Rate(d.WriteBytes)),
			fmt.Sprintf("%8.0f", d.ReadIOPS),
			fmt.Sprintf("%8.0f", d.WriteIOPS),
			fmt.Sprintf("%4.0f%%", d.BusyPercent),
		}
	}
	m.io.SetRows(rows)
}

// View implements ui.Tab.
func (m *Model) View() string {
	if m.width < 1 {
		return ""
	}
	st := m.th.Styles
	head := st.BorderTitle.Render(" FILESYSTEMS ")
	ioHead := st.Muted.Render(" I/O RATES ")
	if m.focus == 1 {
		head = st.Muted.Render(" FILESYSTEMS ")
		ioHead = st.BorderTitle.Render(" I/O RATES ")
	}
	ioHead += st.Muted.Render("  ←→ table  ↑↓ scroll")
	if !m.live {
		head += st.Muted.Render("  waiting for samples…")
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		head,
		m.table.View(),
		"",
		ioHead,
		m.io.View(),
	)
}

func usageColumns(width int) []table.Column {
	fixed := 16 + 26 + 8 + 14 + 8 + 8 + 5
	mountW := max(10, 26+(width-fixed))
	return []table.Column{
		{Title: "DEVICE", Width: 16},
		{Title: "MOUNT", Width: mountW},
		{Title: "TYPE", Width: 8},
		{Title: "USAGE", Width: 14},
		{Title: "USED", Width: 8},
		{Title: "TOTAL", Width: 8},
		{Title: "USE%", Width: 5},
	}
}

func ioColumns(width int) []table.Column {
	_ = width
	return []table.Column{
		{Title: "DEVICE", Width: 16},
		{Title: "READ/s", Width: 9},
		{Title: "WRITE/s", Width: 9},
		{Title: "R IOPS", Width: 8},
		{Title: "W IOPS", Width: 8},
		{Title: "BUSY", Width: 5},
	}
}

// trunc shortens s to at most n-1 runes plus an ellipsis, cutting at
// rune boundaries so multi-byte names stay valid UTF-8.
func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n { // long in bytes, short in runes: nothing to drop
		return s
	}
	return string(r[:n-1]) + "…"
}
