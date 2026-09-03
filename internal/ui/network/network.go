// Package network renders the network tab: per-interface traffic rates
// and totals.
package network

import (
	"cmp"
	"fmt"
	"slices"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ersinkoc/wrongtop/internal/collector"
	"github.com/ersinkoc/wrongtop/internal/config"
	"github.com/ersinkoc/wrongtop/internal/format"
	"github.com/ersinkoc/wrongtop/internal/theme"
)

// Model is the network tab.
type Model struct {
	cfg *config.Config
	th  *theme.Theme

	width, height int
	table         table.Model
	nets          []collector.NetIface
	live          bool
}

// New builds the network tab.
func New(cfg *config.Config, th *theme.Theme) *Model {
	t := table.New(table.WithFocused(true), table.WithWidth(100), table.WithHeight(20))
	t.SetColumns(columns(100)) // sane defaults until SetSize arrives
	return &Model{cfg: cfg, th: th, table: t}
}

// Title implements ui.Tab.
func (m *Model) Title() string { return "NETWORK" }

// SetSize implements ui.Tab.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.table.SetWidth(width)
	m.table.SetHeight(max(3, height-6))
	m.table.SetColumns(columns(width))
}

// Update implements ui.Tab.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case collector.SnapshotMsg:
		m.nets = msg.Snap.Nets
		m.live = true
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
	}
	return nil
}

// rebuild sorts interfaces by current activity (busiest first) and
// refreshes rows.
func (m *Model) rebuild() {
	nets := slices.Clone(m.nets)
	slices.SortFunc(nets, func(a, b collector.NetIface) int {
		return cmp.Compare(a.RxRate+a.TxRate, b.RxRate+b.TxRate) // busiest last→reversed below
	})
	slices.Reverse(nets)

	rows := make([]table.Row, len(nets))
	for i, n := range nets {
		rows[i] = table.Row{
			fmt.Sprintf("%-12s", trunc(n.Name, 12)),
			m.th.Styles.OK.Render(fmt.Sprintf("%10s", format.Rate(n.RxRate))),
			m.th.Styles.Warn.Render(fmt.Sprintf("%10s", format.Rate(n.TxRate))),
			fmt.Sprintf("%11s", format.Bytes(n.RxTotal)),
			fmt.Sprintf("%11s", format.Bytes(n.TxTotal)),
		}
	}
	m.table.SetRows(rows)
}

// View implements ui.Tab.
func (m *Model) View() string {
	if m.width < 1 {
		return ""
	}
	st := m.th.Styles
	var rxRate, txRate float64
	var rxTotal, txTotal uint64
	for _, n := range m.nets {
		rxRate += n.RxRate
		txRate += n.TxRate
		rxTotal += n.RxTotal
		txTotal += n.TxTotal
	}

	head := st.Muted.Render("TOTAL  ") +
		st.OK.Render("↓ "+format.Rate(rxRate)) + st.Muted.Render("  ") +
		st.Warn.Render("↑ "+format.Rate(txRate)) +
		st.Muted.Render(fmt.Sprintf("   ·   total ↓ %s  ↑ %s",
			format.Bytes(rxTotal), format.Bytes(txTotal)))
	if !m.live {
		head += st.Muted.Render("  ·  waiting for samples…")
	}

	footer := st.HelpText.Render("  interfaces sorted by current activity")
	return lipgloss.JoinVertical(lipgloss.Left, head, m.table.View(), footer)
}

func columns(width int) []table.Column {
	_ = width
	return []table.Column{
		{Title: "IFACE", Width: 12},
		{Title: "RX/s", Width: 10},
		{Title: "TX/s", Width: 10},
		{Title: "TOTAL RX", Width: 11},
		{Title: "TOTAL TX", Width: 11},
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
