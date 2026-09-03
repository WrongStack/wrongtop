// Package dashboard renders the wrongtop overview tab: a btop-style
// single-screen grid of host, CPU, memory, network, disk and top-process
// panels, with a glances-style alert strip when thresholds are crossed.
package dashboard

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ersinkoc/wrongtop/internal/collector"
	"github.com/ersinkoc/wrongtop/internal/config"
	"github.com/ersinkoc/wrongtop/internal/theme"
	"github.com/ersinkoc/wrongtop/internal/ui"
	"github.com/ersinkoc/wrongtop/internal/ui/canvas"
)

// Model is the dashboard tab.
type Model struct {
	cfg *config.Config
	th  *theme.Theme

	width, height int

	cpuGraph  *canvas.Graph
	memGraph  *canvas.Graph
	swapGraph *canvas.Graph
	rxGraph   *canvas.Graph
	txGraph   *canvas.Graph

	rxScale canvas.Scale // auto ceilings for the rate graphs
	txScale canvas.Scale

	snap collector.Snapshot
	live bool // at least one snapshot received
}

// New builds the dashboard tab.
func New(cfg *config.Config, th *theme.Theme) *Model {
	return &Model{
		cfg:       cfg,
		th:        th,
		cpuGraph:  canvas.New(60, 3, ramp(th, th.Palette.Green, th.Palette.Yellow, th.Palette.Orange, th.Palette.Red)),
		memGraph:  canvas.New(28, 2, ramp(th, th.Palette.Blue, th.Palette.Purple, th.Palette.Red)),
		swapGraph: canvas.New(28, 1, ramp(th, th.Palette.Purple, th.Palette.Red)),
		rxGraph:   canvas.New(16, 2, ramp(th, th.Palette.Green, th.Palette.Cyan)),
		txGraph:   canvas.New(16, 2, ramp(th, th.Palette.Blue, th.Palette.Cyan)),
	}
}

// Title implements ui.Tab.
func (m *Model) Title() string { return "DASHBOARD" }

// layoutClass picks how the panels arrange at the current size.
type layoutClass int

const (
	layoutGrid    layoutClass = iota // wide and tall: btop-style 3-row grid
	layoutStacked                    // >= 80 cols: two-column rows
	layoutNarrow                     // < 80 cols: single column
)

// Layout knobs. hostCol is the total width of the left column (border
// included); the grid needs both room and rows to pay for three bands.
const (
	hostCol       = 36
	netCol        = 32
	gridMinWidth  = 112
	gridMinHeight = 24
)

func (m *Model) class() layoutClass {
	switch {
	case m.width >= gridMinWidth && m.height >= gridMinHeight:
		return layoutGrid
	case m.width >= 80:
		return layoutStacked
	default:
		return layoutNarrow
	}
}

// SetSize implements ui.Tab.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	if width < 1 || height < 1 {
		return
	}
	cpuW := max(20, width-hostCol-8)
	memW := max(14, hostCol-6)
	rxW := max(10, netCol/2-1)
	if m.class() == layoutNarrow {
		cpuW = max(16, width-6)
		memW = max(12, width/2-8)
		rxW = max(8, width/4-1)
	}
	m.cpuGraph.Resize(cpuW, 3)
	m.memGraph.Resize(memW, 2)
	m.swapGraph.Resize(memW, 1)
	m.rxGraph.Resize(rxW, 2)
	m.txGraph.Resize(rxW, 2)
}

// Update implements ui.Tab.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	snap, ok := msg.(collector.SnapshotMsg)
	if !ok {
		return nil
	}
	m.snap = snap.Snap
	m.live = true

	rx, tx := totalRates(m.snap.Nets)
	m.cpuGraph.Push(clamp01(m.snap.CPU.Percent / 100))
	m.memGraph.Push(clamp01(m.snap.Mem.Percent / 100))
	m.swapGraph.Push(clamp01(m.snap.Mem.SwapPercent / 100))
	m.rxGraph.Push(clamp01(rx / m.rxScale.Observe(rx)))
	m.txGraph.Push(clamp01(tx / m.txScale.Observe(tx)))
	return nil
}

// View implements ui.Tab.
func (m *Model) View() string {
	if m.width < 1 {
		return ""
	}
	if !m.live {
		wait := ui.Box(lipgloss.RoundedBorder(), m.th.Styles.Border, m.th.Styles.BorderChar,
			m.th.Styles.BorderTitle, "WRONGTOP", m.th.Styles.Muted.Render("waiting for samples…"))
		return wait
	}

	var rows []string
	if strip := m.alertsView(); strip != "" {
		rows = append(rows, strip)
	}
	if m.class() == layoutGrid {
		rows = append(rows, m.gridRows()...)
	} else {
		rows = append(rows, m.stackedRows()...)
	}
	return strings.Join(rows, "\n")
}

// gridRows lays out the three bands: HOST|CPU, MEMORY|NETWORK|DISKS and a
// full-width PROCESSES panel. Band heights come from the content height;
// when the process panel cannot pay for itself the two upper bands split
// the space instead.
func (m *Model) gridRows() []string {
	w, h := m.width, m.height
	remaining := h - 2 - alertLines(m) // row gaps
	rowA := clampInt(remaining*4/10, 9, 12)
	rowB := clampInt(remaining*3/10, 7, 10)
	rowC := remaining - rowA - rowB
	if rowC < 5 {
		rowC = 0
		rowA = clampInt(remaining*5/9, 9, 13)
		rowB = remaining - rowA
	}

	cpuInner := w - hostCol - 4
	memInner := hostCol - 2
	diskInner := w - hostCol - netCol - 4
	// the network panel is 1 head + 2+2 graph rows + iface lines
	ifaceN := min(ifaceBudget(h), max(0, rowB-7))

	row1 := lipgloss.JoinHorizontal(lipgloss.Top,
		m.box(memInner, "HOST", padLines(m.hostView(), rowA-2)),
		" ",
		m.box(cpuInner, "CPU", padLines(m.cpuView(cpuInner, rowA-2), rowA-2)),
	)
	row2 := lipgloss.JoinHorizontal(lipgloss.Top,
		m.box(memInner, "MEMORY", padLines(m.memView(memInner), rowB-2)),
		" ",
		m.box(netCol-2, "NETWORK", padLines(m.netView(ifaceN), rowB-2)),
		" ",
		m.box(diskInner, "DISKS", padLines(m.diskView(diskInner, rowB-3), rowB-2)),
	)
	rows := []string{row1, "", row2}
	if rowC > 0 {
		rows = append(rows, "", m.box(w-2, "PROCESSES", strings.Join(m.procView(w-6, rowC-3), "\n")))
	}
	return rows
}

// stackedRows arranges the same panels in as few rows as the width
// allows, dropping the process panel first when height runs out.
func (m *Model) stackedRows() []string {
	w, h := m.width, m.height
	coreRows := clampInt((h-14)/3, 1, 5)
	ifaceRows := ifaceBudget(h)
	diskRows := clampInt((h-12)/4, 2, 6)
	procRows := clampInt((h-26)/2, 0, 7)

	cpuInner := w - hostCol - 4
	if m.class() == layoutNarrow {
		cpuInner = w - 4
	}

	var rows []string
	if m.class() == layoutNarrow {
		rows = append(rows,
			m.box(w-2, "HOST", strings.Join(m.hostView(), "\n")), "",
			m.box(w-2, "CPU", strings.Join(m.cpuView(cpuInner, coreRows+4), "\n")), "",
		)
	} else {
		rows = append(rows,
			lipgloss.JoinHorizontal(lipgloss.Top,
				m.box(hostCol-2, "HOST", padLines(m.hostView(), coreRows+4)),
				" ",
				m.box(cpuInner, "CPU", padLines(m.cpuView(cpuInner, coreRows+4), coreRows+4)),
			), "",
		)
	}

	memInner := max(14, hostCol-6)
	if m.class() == layoutNarrow {
		memInner = max(12, w/2-8)
	}
	rows = append(rows, m.box(max(20, 2*memInner+2), "MEMORY", strings.Join(m.memView(memInner), "\n")), "")

	if m.class() == layoutNarrow {
		rows = append(rows,
			m.box(w-2, "NETWORK", strings.Join(m.netView(ifaceRows), "\n")), "",
			m.box(w-2, "DISKS", strings.Join(m.diskView(w-6, diskRows+1), "\n")), "",
		)
	} else {
		netInner := netCol - 2
		diskInner := w - netCol - 4
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top,
			m.box(netInner, "NETWORK", padLines(m.netView(ifaceRows), diskRows+1)),
			" ",
			m.box(diskInner, "DISKS", padLines(m.diskView(diskInner, diskRows+1), diskRows+1)),
		), "")
	}

	if procRows >= 2 {
		rows = append(rows, m.box(w-2, "PROCESSES", strings.Join(m.procView(w-6, procRows), "\n")))
	}
	return rows
}

// box renders a titled border around content padded to exactly innerW
// cells so grid columns line up.
func (m *Model) box(innerW int, title, content string) string {
	padded := lipgloss.NewStyle().Width(innerW).Render(content)
	return ui.Box(lipgloss.RoundedBorder(), m.th.Styles.Border, m.th.Styles.BorderChar,
		m.th.Styles.BorderTitle, title, padded)
}

// alertLines reports how many rows the alert strip occupies.
func alertLines(m *Model) int {
	if len(m.alerts()) > 0 {
		return 1
	}
	return 0
}

func (m *Model) kv(key, val string) string {
	return m.th.Styles.Muted.Render(fmt.Sprintf("%-8s", key)) + val
}

func ramp(th *theme.Theme, hex ...string) []color.Color {
	out := make([]color.Color, len(hex))
	for i, h := range hex {
		out[i] = lipgloss.Color(h)
	}
	return out
}

func clamp01(v float64) float64 { return min(max(v, 0), 1) }

func clampInt(v, lo, hi int) int { return min(max(v, lo), hi) }
