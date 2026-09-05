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
	usageRamp     canvas.Ramp

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
	return &Model{
		cfg:       cfg,
		th:        th,
		table:     t,
		io:        io,
		usageRamp: canvas.Ramp{th.Palette.Green, th.Palette.Yellow, th.Palette.Orange, th.Palette.Red},
	}
}

// Title implements ui.Tab.
func (m *Model) Title() string { return "▤ DISKS" }

// SetTheme implements ui.Tab.
func (m *Model) SetTheme(th *theme.Theme) {
	m.th = th
	m.usageRamp = canvas.Ramp{th.Palette.Green, th.Palette.Yellow, th.Palette.Orange, th.Palette.Red}
}

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
	full := usageMode(m.width) == 2 // TYPE + USED/TOTAL columns present
	for i, d := range m.disks {
		style := m.th.Value(m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit, d.Percent)
		row := table.Row{
			ui.Trunc(d.Device, 16),
			ui.Trunc(d.Mountpoint, 26),
		}
		if full {
			row = append(row, ui.Trunc(d.FSType, 8))
		}
		row = append(row,
			style.Render(canvas.GradientBar(14, d.Percent/100, m.usageRamp, m.th.Styles.Muted)))
		if full {
			// compact byte formatting keeps wide counts inside the
			// 8-cell columns ("128 GiB" instead of a clipped "999.9 GiB")
			row = append(row,
				fmt.Sprintf("%7s", format.BytesCompact(d.Used)),
				fmt.Sprintf("%7s", format.BytesCompact(d.Total)))
		}
		row = append(row, style.Render(fmt.Sprintf("%4.0f%%", d.Percent)))
		rows[i] = row
	}
	m.table.SetRows(rows)

	rows = make([]table.Row, len(m.diskIOs))
	iops := m.width >= 76
	for i, d := range m.diskIOs {
		row := table.Row{
			ui.Trunc(d.Name, 16),
			fmt.Sprintf("%9s", format.Rate(d.ReadBytes)),
			fmt.Sprintf("%9s", format.Rate(d.WriteBytes)),
		}
		if iops {
			row = append(row,
				fmt.Sprintf("%8.0f", d.ReadIOPS),
				fmt.Sprintf("%8.0f", d.WriteIOPS))
		}
		row = append(row, fmt.Sprintf("%4.0f%%", d.BusyPercent))
		rows[i] = row
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

// usageMode picks which FILESYSTEMS columns fit the terminal: TYPE
// drops first, then USED/TOTAL. Rows (rebuild) and headers (columns)
// both consult it so the two never disagree.
func usageMode(width int) int {
	switch {
	case width >= 100:
		return 2
	case width >= 76:
		return 1
	default:
		return 0
	}
}

func usageColumns(width int) []table.Column {
	fixed := 16 + 14 + 5 // DEVICE + USAGE + USE%
	if usageMode(width) == 2 {
		fixed += 8 + 8 + 8 // TYPE + USED + TOTAL
	}
	mountW := max(10, 26+(width-fixed))
	cols := []table.Column{
		{Title: "DEVICE", Width: 16},
		{Title: "MOUNT", Width: mountW},
	}
	if usageMode(width) == 2 {
		cols = append(cols, table.Column{Title: "TYPE", Width: 8})
	}
	cols = append(cols, table.Column{Title: "USAGE", Width: 14})
	if usageMode(width) == 2 {
		cols = append(cols,
			table.Column{Title: "USED", Width: 8},
			table.Column{Title: "TOTAL", Width: 8})
	}
	return append(cols, table.Column{Title: "USE%", Width: 5})
}

func ioColumns(width int) []table.Column {
	cols := []table.Column{
		{Title: "DEVICE", Width: 16},
		{Title: "READ/s", Width: 9},
		{Title: "WRITE/s", Width: 9},
	}
	if width >= 76 { // IOPS figures are the first to go on narrow terms
		cols = append(cols,
			table.Column{Title: "R IOPS", Width: 8},
			table.Column{Title: "W IOPS", Width: 8})
	}
	return append(cols, table.Column{Title: "BUSY", Width: 5})
}
