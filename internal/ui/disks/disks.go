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
	"github.com/ersinkoc/wrongtop/internal/ui/canvas"
)

// Model is the disks tab.
type Model struct {
	cfg *config.Config
	th  *theme.Theme

	width, height int
	table         table.Model
	io            table.Model

	disks   []collector.Disk
	diskIOs []collector.DiskIO
	live    bool
}

// New builds the disks tab.
func New(cfg *config.Config, th *theme.Theme) *Model {
	t := table.New(table.WithFocused(true), table.WithWidth(100), table.WithHeight(12))
	io := table.New(table.WithFocused(false), table.WithWidth(100), table.WithHeight(8))
	return &Model{cfg: cfg, th: th, table: t, io: io}
}

// Title implements ui.Tab.
func (m *Model) Title() string { return "DISKS" }

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
		if mouse.Button == tea.MouseWheelUp {
			m.table.MoveUp(3)
		} else if mouse.Button == tea.MouseWheelDown {
			m.table.MoveDown(3)
		}
		return nil
	}
	return nil
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
	ioHead := st.BorderTitle.Render(" I/O RATES ")
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

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
