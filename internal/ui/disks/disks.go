// Package disks renders the disks tab: filesystem usage and per-device
// I/O rates.
package disks

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/format"
	"github.com/wrongstack/wrongtop/internal/theme"
	"github.com/wrongstack/wrongtop/internal/ui"
	"github.com/wrongstack/wrongtop/internal/ui/canvas"
)

// activitySamples is the history length of the per-device ACTIVITY
// sparklines; activityWidth is the column's cell width.
const (
	activitySamples = 12
	activityWidth   = 12
)

// Model is the disks tab.
type Model struct {
	cfg *config.Config
	th  *theme.Theme

	width, height int
	table         table.Model
	io            table.Model
	focus         int // 0 = filesystem table, 1 = io table

	hist    map[string][]float64 // per-device total I/O rate history
	disks   []collector.Disk
	diskIOs []collector.DiskIO
	live    bool
	visible bool // the active tab; hidden tabs skip table rebuilds
}

// New builds the disks tab.
func New(cfg *config.Config, th *theme.Theme) *Model {
	t := table.New(table.WithFocused(true), table.WithWidth(100), table.WithHeight(12))
	io := table.New(table.WithFocused(false), table.WithWidth(100), table.WithHeight(8))
	t.SetColumns(usageColumns(100)) // sane defaults until SetSize arrives
	io.SetColumns(ioColumns(100, true))
	m := &Model{cfg: cfg, th: th, table: t, io: io, hist: make(map[string][]float64), visible: true}
	m.applyTableStyles()
	return m
}

// SetVisible implements ui.Tab. Hidden tabs keep their history warm but
// skip the row rebuild; activation catches up.
func (m *Model) SetVisible(visible bool) {
	m.visible = visible
	if visible {
		m.rebuild()
	}
}

// applyTableStyles themes both tables: muted headers, and the soft
// accent fill follows whichever table owns the focus.
func (m *Model) applyTableStyles() {
	muted := table.DefaultStyles()
	muted.Header = muted.Header.Foreground(lipgloss.Color(m.th.Palette.Gray))

	focused := muted
	focused.Selected = m.th.Styles.Selected.Padding(0, 1)

	m.table.SetStyles(muted)
	m.io.SetStyles(muted)
	if m.focus == 1 {
		m.io.SetStyles(focused)
	} else {
		m.table.SetStyles(focused)
	}
}

// Title implements ui.Tab.
func (m *Model) Title() string { return ui.Icon("disk", m.cfg.NerdFonts) + " DISKS" }

// SetTheme implements ui.Tab.
func (m *Model) SetTheme(th *theme.Theme) {
	m.th = th
	m.applyTableStyles()
}

// SetSize implements ui.Tab.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	h := max(3, (height-8)/2)
	m.table.SetWidth(width)
	m.table.SetHeight(h)
	m.io.SetWidth(width)
	m.io.SetHeight(max(2, height-8-h))
	ui.SetTableColumns(&m.table, usageColumns(width))
	ui.SetTableColumns(&m.io, ioColumns(width, width >= 90))
	m.rebuild() // resize can change the column shape; rows must follow
}

// Update implements ui.Tab.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case collector.SnapshotMsg:
		m.disks = msg.Snap.Disks
		m.diskIOs = msg.Snap.DiskIOs
		m.live = true
		m.recordIO()
		if m.visible {
			m.rebuild()
		}
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
			m.applyTableStyles() // the selection fill follows the focus
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

// recordIO appends one total-rate sample per device for the ACTIVITY
// sparklines; the map is bounded against device-name churn.
func (m *Model) recordIO() {
	if len(m.hist) > 128 {
		m.hist = make(map[string][]float64)
	}
	for _, d := range m.diskIOs {
		h := append(m.hist[d.Name], d.ReadBytes+d.WriteBytes)
		if len(h) > activitySamples {
			h = h[len(h)-activitySamples:]
		}
		m.hist[d.Name] = h
	}
}

// showActivity reports whether the I/O table has room for the ACTIVITY
// sparkline column.
func (m *Model) showActivity() bool { return m.width >= 90 }

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
			style.Render(canvas.GradientBar(14, d.Percent/100, m.th.Ramps.CPU, m.th.Styles.Track)))
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
			fmt.Sprintf("%10s", format.Rate(d.ReadBytes)),
			fmt.Sprintf("%10s", format.Rate(d.WriteBytes)),
		}
		if m.showActivity() {
			spark := canvas.SparklineScaled(m.hist[d.Name], m.th.Ramps.IO)
			if pad := activityWidth - lipgloss.Width(spark); pad > 0 {
				spark += strings.Repeat(" ", pad) // keep the cell at column width
			}
			row = append(row, spark)
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

func ioColumns(width int, activity bool) []table.Column {
	cols := []table.Column{
		{Title: "DEVICE", Width: 16},
		{Title: "READ/s", Width: 10},
		{Title: "WRITE/s", Width: 10},
	}
	if activity {
		cols = append(cols, table.Column{Title: "ACTIVITY", Width: activityWidth})
	}
	if width >= 76 { // IOPS figures are the first to go on narrow terms
		cols = append(cols,
			table.Column{Title: "R IOPS", Width: 8},
			table.Column{Title: "W IOPS", Width: 8})
	}
	return append(cols, table.Column{Title: "BUSY", Width: 5})
}
