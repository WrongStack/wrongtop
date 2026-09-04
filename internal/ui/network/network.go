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
	"github.com/ersinkoc/wrongtop/internal/ui"
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
func (m *Model) Title() string { return "⇅ NETWORK" }

// SetTheme implements ui.Tab.
func (m *Model) SetTheme(th *theme.Theme) { m.th = th }

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
