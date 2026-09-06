// Package network renders the network tab: per-interface traffic rates
// and totals.
package network

import (
	"cmp"
	"fmt"
	"slices"
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

// Model is the network tab.
type Model struct {
	cfg *config.Config
	th  *theme.Theme

	width, height int
	table         table.Model
	nets          []collector.NetIface
	live          bool
	visible       bool                 // the active tab; hidden tabs skip table rebuilds
	hist          map[string][]float64 // per-interface total-rate history
}

// actWidth is the width of the MIX column in cells.
const (
	actWidth = 20
	// activitySamples is the history length of the ACTIVITY sparklines.
	activitySamples = 12
)

// New builds the network tab.
func New(cfg *config.Config, th *theme.Theme) *Model {
	t := table.New(table.WithFocused(true), table.WithWidth(100), table.WithHeight(20))
	t.SetColumns(columns(100)) // sane defaults until SetSize arrives
	m := &Model{cfg: cfg, th: th, table: t, hist: make(map[string][]float64), visible: true}
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

// applyTableStyles themes the table: muted header, soft accent fill on
// the focused row.
func (m *Model) applyTableStyles() {
	ts := table.DefaultStyles()
	ts.Header = ts.Header.Foreground(lipgloss.Color(m.th.Palette.Gray))
	ts.Selected = m.th.Styles.Selected.Padding(0, 1)
	m.table.SetStyles(ts)
}

// Title implements ui.Tab.
func (m *Model) Title() string { return ui.Icon("net", m.cfg.NerdFonts) + " NETWORK" }

// SetTheme implements ui.Tab.
func (m *Model) SetTheme(th *theme.Theme) {
	m.th = th
	m.applyTableStyles()
}

// SetSize implements ui.Tab.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.table.SetWidth(width)
	m.table.SetHeight(max(3, height-6))
	ui.SetTableColumns(&m.table, columns(width))
	m.rebuild() // resize can change the column shape; rows must follow
}

// Update implements ui.Tab.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case collector.SnapshotMsg:
		m.nets = msg.Snap.Nets
		m.live = true
		m.recordRates()
		if m.visible {
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
	}
	return nil
}

// recordRates appends one total-rate sample per interface for the
// ACTIVITY sparklines; the map is bounded against interface churn
// (VPNs, Wi-Fi hotspots).
func (m *Model) recordRates() {
	if len(m.hist) > 128 {
		m.hist = make(map[string][]float64)
	}
	for _, n := range m.nets {
		h := append(m.hist[n.Name], n.RxRate+n.TxRate)
		if len(h) > activitySamples {
			h = h[len(h)-activitySamples:]
		}
		m.hist[n.Name] = h
	}
}

// rebuild sorts interfaces by current activity (busiest first) and
// refreshes rows.
func (m *Model) rebuild() {
	nets := slices.Clone(m.nets)
	slices.SortFunc(nets, func(a, b collector.NetIface) int {
		return cmp.Compare(a.RxRate+a.TxRate, b.RxRate+b.TxRate) // busiest last→reversed below
	})
	slices.Reverse(nets)

	totals := m.width >= 100
	drops := m.width >= 112
	activity := m.width >= 88
	rows := make([]table.Row, len(nets))
	for i, n := range nets {
		row := table.Row{
			fmt.Sprintf("%-12s", ui.Trunc(n.Name, 12)),
			m.th.Styles.OK.Render(fmt.Sprintf("%10s", format.Rate(n.RxRate))),
			m.th.Styles.Warn.Render(fmt.Sprintf("%10s", format.Rate(n.TxRate))),
		}
		if totals {
			row = append(row,
				fmt.Sprintf("%11s", format.Bytes(n.RxTotal)),
				fmt.Sprintf("%11s", format.Bytes(n.TxTotal)))
		}
		if drops {
			cell := m.th.Styles.Muted.Render(fmt.Sprintf("%7.1f", n.RxDrop+n.TxDrop))
			if n.RxDrop+n.TxDrop > 0.5 {
				cell = m.th.Styles.Warn.Render(fmt.Sprintf("%7.1f", n.RxDrop+n.TxDrop))
			}
			row = append(row, cell)
		}
		if activity {
			spark := canvas.SparklineScaled(m.hist[n.Name], m.th.Ramps.RX)
			if pad := 12 - lipgloss.Width(spark); pad > 0 {
				spark += strings.Repeat(" ", pad)
			}
			row = append(row, spark)
		}
		// glances-style mixed meter: the green share of the bar is the
		// download fraction, the blue share the upload fraction
		total := n.RxRate + n.TxRate
		share := 0.5
		if total > 0 {
			share = n.RxRate / total
		}
		row = append(row, canvas.DualBar(actWidth, share, m.th.Ramps.RX, m.th.Ramps.TX))
		rows[i] = row
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
	cols := []table.Column{
		{Title: "IFACE", Width: 12},
		{Title: "RX/s", Width: 10},
		{Title: "TX/s", Width: 10},
	}
	if width >= 100 { // lifetime totals are the first to go on narrow terms
		cols = append(cols,
			table.Column{Title: "TOTAL RX", Width: 11},
			table.Column{Title: "TOTAL TX", Width: 11})
	}
	if width >= 112 {
		cols = append(cols, table.Column{Title: "DROP/s", Width: 8})
	}
	if width >= 88 {
		cols = append(cols, table.Column{Title: "ACTIVITY", Width: 12})
	}
	return append(cols, table.Column{Title: "MIX ↓↑", Width: actWidth})
}
