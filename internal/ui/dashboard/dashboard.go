// Package dashboard renders the wrongtop overview tab: a btop-style
// single-screen grid of host, CPU, memory, network, disk and top-process
// panels. Each panel carries its live readout on the border line —
// title left, value right — with alert chips in the app-level tab bar.
package dashboard

import (
	"fmt"
	"strings"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/theme"
	"github.com/wrongstack/wrongtop/internal/ui"
	"github.com/wrongstack/wrongtop/internal/ui/canvas"
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
	loadGraph *canvas.Graph

	coreGraphs map[int]*canvas.Graph // rolling graph per logical core

	rxScale canvas.Scale // auto ceilings for the rate graphs
	txScale canvas.Scale

	snap collector.Snapshot
	live bool // at least one snapshot received

	// view cache: View() runs after every event (mouse, keys, focus),
	// but the frame only depends on snapshot/size/theme/density — so it
	// is rebuilt lazily and reused otherwise.
	viewCache string

	// per-device total I/O rate history for the disks panel sparklines
	ioHist map[string][]float64

	density int // 0 full, 1 compact (fewer graphs), 2 minimal (HOST+CPU)
}

// Density presets cycle with p and start from config layout.
const (
	densityFull = iota
	densityCompact
	densityMinimal
)

// New builds the dashboard tab. Graph ramps come from the theme so the
// graphs and the meters always read the same palette story.
func New(cfg *config.Config, th *theme.Theme) *Model {
	density := densityFull
	switch cfg.Layout {
	case "compact":
		density = densityCompact
	case "minimal":
		density = densityMinimal
	}
	return &Model{
		cfg:        cfg,
		th:         th,
		density:    density,
		ioHist:     make(map[string][]float64),
		coreGraphs: make(map[int]*canvas.Graph),
		cpuGraph:   canvas.New(60, 3, th.Ramps.CPU.Colors()),
		memGraph:   canvas.New(34, 2, th.Ramps.Mem.Colors()),
		swapGraph:  canvas.New(34, 1, th.Ramps.Swap.Colors()),
		rxGraph:    canvas.New(netCol-2, 2, th.Ramps.RX.Colors()),
		txGraph:    canvas.New(netCol-2, 2, th.Ramps.TX.Colors()),
		loadGraph:  canvas.New(34, 2, th.Ramps.Load.Colors()),
	}
}

// Title implements ui.Tab.
func (m *Model) Title() string { return ui.Icon("host", m.cfg.NerdFonts) + " DASHBOARD" }

// SetVisible implements ui.Tab. The dashboard keeps pushing graph
// history while hidden (that is the point of a warm dashboard); its
// view cache already makes rebuilds lazy.
func (m *Model) SetVisible(bool) {}

// SetTheme implements ui.Tab; graph ramps derive from the palette so
// they must be rebuilt alongside the styles.
func (m *Model) SetTheme(th *theme.Theme) {
	m.th = th
	m.viewCache = ""
	m.cpuGraph.SetRamp(th.Ramps.CPU.Colors())
	m.memGraph.SetRamp(th.Ramps.Mem.Colors())
	m.swapGraph.SetRamp(th.Ramps.Swap.Colors())
	m.rxGraph.SetRamp(th.Ramps.RX.Colors())
	m.txGraph.SetRamp(th.Ramps.TX.Colors())
	m.loadGraph.SetRamp(th.Ramps.Load.Colors())
	for _, g := range m.coreGraphs {
		g.SetRamp(th.Ramps.CPU.Colors())
	}
}

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
	m.viewCache = ""
	if width < 1 || height < 1 {
		return
	}
	cpuW := max(20, width-hostCol-8)
	if m.density == densityFull && m.class() == layoutGrid {
		// the wide CPU panel: full-width total graph with the per-core
		// graph grid beneath it
		cpuW = max(20, width-hostCol-2)
	}
	memW := max(14, hostCol-2)
	rxW := max(10, netCol-2)
	if m.class() == layoutNarrow {
		cpuW = max(16, width-6)
		memW = max(12, width/2-8)
		rxW = max(12, width-6)
	}
	m.cpuGraph.Resize(cpuW, 3)
	for _, g := range m.coreGraphs {
		g.Resize(m.coreGraphW(width-hostCol-2), 2)
	}
	m.memGraph.Resize(memW, 2)
	m.swapGraph.Resize(memW, 1)
	m.rxGraph.Resize(rxW, 2)
	m.txGraph.Resize(rxW, 2)
	m.loadGraph.Resize(max(16, hostCol-2), 2)
}

// Update implements ui.Tab.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case collector.SnapshotMsg:
		m.snap = msg.Snap
		m.live = true
		m.recordIO()
		m.recordCores()
		m.viewCache = "" // new data: rebuild lazily on the next View

		cores := float64(max(1, len(m.snap.CPU.Cores)))
		m.loadGraph.Push(clamp01(m.snap.Host.Load[0] / cores))
		rx, tx := totalRates(m.snap.Nets)
		m.cpuGraph.Push(clamp01(m.snap.CPU.Percent / 100))
		m.memGraph.Push(clamp01(m.snap.Mem.Percent / 100))
		m.swapGraph.Push(clamp01(m.snap.Mem.SwapPercent / 100))
		m.rxGraph.Push(clamp01(rx / m.rxScale.Observe(rx)))
		m.txGraph.Push(clamp01(tx / m.txScale.Observe(tx)))
		return nil

	case tea.KeyPressMsg:
		if msg.String() == "p" { // cycle density presets
			m.density = (m.density + 1) % 3
			m.viewCache = ""
		}
		return nil
	}
	return nil
}

// ioSparkSamples is the history length of the per-device sparklines in
// the DISKS panel.
const ioSparkSamples = 10

// recordIO appends one total-rate sample per device for the disks panel
// sparklines. The map is bounded: a machine churning device names (USB
// docks, disk images) cannot grow it without limit.
func (m *Model) recordIO() {
	if len(m.ioHist) > 128 {
		m.ioHist = make(map[string][]float64)
	}
	for _, d := range m.snap.DiskIOs {
		h := append(m.ioHist[d.Name], d.ReadBytes+d.WriteBytes)
		if len(h) > ioSparkSamples {
			h = h[len(h)-ioSparkSamples:]
		}
		m.ioHist[d.Name] = h
	}
}

// recordCores feeds one sample into every per-core rolling graph,
// creating graphs on demand (the core count is unknown until the first
// snapshot) and pruning graphs of cores that no longer exist.
func (m *Model) recordCores() {
	w := m.coreGraphW(m.width - hostCol - 2)
	for i, pct := range m.snap.CPU.Cores {
		g, ok := m.coreGraphs[i]
		if !ok {
			g = canvas.New(w, 2, m.th.Ramps.CPU.Colors())
			m.coreGraphs[i] = g
		}
		g.Push(clamp01(pct / 100))
	}
	for i := range m.coreGraphs {
		if i >= len(m.snap.CPU.Cores) {
			delete(m.coreGraphs, i)
		}
	}
}

// coreGraphW sizes the per-core graphs: six cells per row is the sweet
// spot, each cell spending ~8 cells on its label and value.
func (m *Model) coreGraphW(innerW int) int {
	return clampInt(innerW/6-8, 8, 24)
}

// View implements ui.Tab. The rendered frame is cached: View runs after
// every event (mouse, keys, focus), but the output only changes when a
// snapshot lands or the size/theme/density changes.
func (m *Model) View() string {
	if m.width < 1 {
		return ""
	}
	if !m.live {
		wait := ui.Box(ui.BorderFor(m.cfg.Border), m.th.Styles.Border, m.th.Styles.BorderChar,
			m.th.Styles.BorderTitle, "WRONGTOP", "", m.th.Styles.Muted.Render("waiting for samples…"))
		return wait
	}
	if m.viewCache != "" {
		return m.viewCache
	}
	m.viewCache = m.buildView()
	return m.viewCache
}

// buildView renders the full dashboard once per snapshot. Threshold
// alerts live in the tab bar now (app level), so the grid always gets
// the full height.
func (m *Model) buildView() string {
	// the btop-style connected grid: one frame, shared dividers
	if m.class() == layoutGrid {
		return ui.Frame(m.gridPanels(m.height), m.th.Styles.BorderChar,
			ui.BorderFor(m.cfg.Border))
	}

	var rows []string
	rows = append(rows, m.stackedRows()...)
	return strings.Join(rows, "\n")
}

// gridPanels lays out the connected frame: HOST|CPU, MEMORY|NETWORK|DISKS
// and a full-width PROCESSES panel, with a GPU band inserted above the
// process panel when adapters are present. Adjacent panels overlap by
// exactly one border column/row, so dividers are single shared lines.
func (m *Model) gridPanels(h int) []ui.Panel {
	w := m.width
	a := clampInt(h*4/10, 9, 12) // band A rows (0..a-1)
	b := clampInt(h*3/10, 7, 10) // band B rows (a-1..a+b-2)
	c := h - a - b + 2           // band C shares two border rows
	if c < 6 {                   // the process panel cannot pay for itself
		c = 0
		a = clampInt(h*5/9, 9, 13)
		b = h - a + 1
	}

	// divider columns at x=hostW and x=hostW+netW
	hostW := hostCol
	netW := netCol
	cpuInner := w - hostW - 2

	panels := []ui.Panel{
		{X: 0, Y: 0, W: hostW + 1, H: a, Title: m.panelTitle("host", "HOST"),
			TitleStyle: m.catTitle("host"), Right: fitRight(hostW+1, m.panelTitle("host", "HOST"), m.hostRight()),
			Border: m.catBorder("host"), Lines: m.hostView(hostW-2, a-2)},
		{X: hostW, Y: 0, W: w - hostW, H: a, Title: m.panelTitle("cpu", "CPU"),
			TitleStyle: m.catTitle("cpu"), Right: fitRight(w-hostW, m.panelTitle("cpu", "CPU"), m.cpuRight()),
			Border: m.catBorder("cpu"), Lines: m.cpuView(cpuInner, a-2)},
	}
	if m.density == densityMinimal {
		return panels
	}

	panels = append(panels,
		ui.Panel{X: 0, Y: a - 1, W: hostW + 1, H: b, Title: m.panelTitle("mem", "MEMORY"),
			TitleStyle: m.catTitle("mem"), Right: fitRight(hostW+1, m.panelTitle("mem", "MEMORY"), m.memRight()),
			Border: m.catBorder("mem"), Lines: m.memView(hostW - 2)},
		ui.Panel{X: hostW, Y: a - 1, W: netW + 1, H: b, Title: m.panelTitle("net", "NETWORK"),
			TitleStyle: m.catTitle("net"),
			Border:     m.catBorder("net"), Lines: m.netView(max(0, b-7))},
		ui.Panel{X: hostW + netW, Y: a - 1, W: w - hostW - netW, H: b, Title: m.panelTitle("disk", "DISKS"),
			TitleStyle: m.catTitle("disk"), Right: fitRight(w-hostW-netW, m.panelTitle("disk", "DISKS"), m.diskRight()),
			Border: m.catBorder("disk"), Lines: m.diskView(w-hostW-netW-2, max(2, b-2))},
	)
	if c == 0 {
		return panels
	}

	procY, procH := a+b-2, c
	if gpus := len(m.snap.GPUs); gpus > 0 && c >= 8 {
		gh := min(gpus+3, 6) // up to four adapters + borders
		panels = append(panels, ui.Panel{X: 0, Y: procY, W: w, H: gh, Title: m.panelTitle("gpu", "GPUS"),
			TitleStyle: m.catTitle("gpu"), Right: fitRight(w, m.panelTitle("gpu", "GPUS"), m.gpuRight()),
			Border: m.catBorder("gpu"), Lines: m.gpuLines(w-2, gh-2)})
		procY += gh - 1 // share the border row
		procH -= gh - 1
	}
	panels = append(panels, ui.Panel{X: 0, Y: procY, W: w, H: procH, Title: m.panelTitle("proc", "PROCESSES"),
		TitleStyle: m.catTitle("proc"), Right: fitRight(w, m.panelTitle("proc", "PROCESSES"), m.procRight()),
		Border: m.catBorder("proc"), Lines: m.procView(w-2, max(2, procH-3))})
	return panels
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
			m.box(w-2, "host", "HOST", m.hostRight(), strings.Join(m.hostView(w-4, 99), "\n")), "",
			m.box(w-2, "cpu", "CPU", m.cpuRight(), strings.Join(m.cpuView(cpuInner, coreRows+4), "\n")), "",
		)
	} else {
		rows = append(rows,
			lipgloss.JoinHorizontal(lipgloss.Top,
				m.box(hostCol-2, "host", "HOST", m.hostRight(), padLines(m.hostView(hostCol-4, coreRows+4), coreRows+4)),
				" ",
				m.box(cpuInner, "cpu", "CPU", m.cpuRight(), padLines(m.cpuView(cpuInner, coreRows+4), coreRows+4)),
			), "",
		)
	}

	memInner := max(14, hostCol-6)
	if m.class() == layoutNarrow {
		memInner = max(12, w/2-8)
	}
	if m.density == densityMinimal {
		return rows // HOST|CPU only
	}
	rows = append(rows, m.box(max(20, 2*memInner+2), "mem", "MEMORY", m.memRight(),
		strings.Join(m.memView(memInner), "\n")), "")

	// the network and disk boxes share a row: both pad to the taller of
	// the two content budgets so their borders align
	netH := max(5+ifaceRows, diskRows+2)
	if m.class() == layoutNarrow {
		rows = append(rows,
			m.box(w-2, "net", "NETWORK", "", strings.Join(m.netView(ifaceRows), "\n")), "",
			m.box(w-2, "disk", "DISKS", m.diskRight(), strings.Join(m.diskView(w-6, diskRows+1), "\n")), "",
		)
	} else {
		netInner := netCol - 2
		diskInner := w - netCol - 4
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top,
			m.box(netInner, "net", "NETWORK", "", padLines(m.netView(ifaceRows), netH)),
			" ",
			m.box(diskInner, "disk", "DISKS", m.diskRight(), padLines(m.diskView(diskInner, netH), netH)),
		), "")
	}

	if procRows >= 2 {
		if gpus := len(m.snap.GPUs); gpus > 0 && procRows >= 5 {
			gpuRows := min(3, gpus)
			rows = append(rows,
				m.box(w-2, "gpu", "GPUS", m.gpuRight(), strings.Join(m.gpuLines(w-6, gpuRows), "\n")), "")
			procRows -= gpuRows + 1
		}
		rows = append(rows, m.box(w-2, "proc", "PROCESSES", m.procRight(),
			strings.Join(m.procView(w-6, procRows), "\n")))
	}
	return rows
}

// catColor maps a panel category to its palette accent — the same color
// on the border, the title and the graph chrome, grid or stacked.
func (m *Model) catColor(kind string) string {
	pal := m.th.Palette
	switch kind {
	case "host":
		return pal.Blue
	case "cpu":
		return pal.Green
	case "mem":
		return pal.Purple
	case "net":
		return pal.Cyan
	case "disk":
		return pal.Orange
	case "gpu":
		return pal.Red
	default:
		return pal.FG
	}
}

func (m *Model) catTitle(kind string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(m.catColor(kind))).Bold(true)
}

func (m *Model) catBorder(kind string) *lipgloss.Style {
	st := lipgloss.NewStyle().Foreground(lipgloss.Color(m.catColor(kind)))
	return &st
}

// panelTitle renders "⌂ HOST" with the configured icon set.
func (m *Model) panelTitle(kind, label string) string {
	return ui.Icon(kind, m.cfg.NerdFonts) + " " + label
}

// fitRight drops the border readout when title and value would crowd
// the panel top — a tight border line reads worse than no readout.
// The margin covers the title's surrounding spaces plus a visible fill.
func fitRight(panelW int, title, right string) string {
	if right == "" || lipgloss.Width(title)+lipgloss.Width(right)+4 > panelW {
		return ""
	}
	return right
}

// box renders a titled, category-colored border around content padded
// to exactly innerW cells so grid columns line up.
func (m *Model) box(innerW int, kind, title, right, content string) string {
	padded := lipgloss.NewStyle().Width(innerW).Render(content)
	label := m.panelTitle(kind, title)
	if lipgloss.Width(label)+lipgloss.Width(right)+6 > innerW {
		right = "" // never let the chrome stretch the box past the terminal
	}
	col := m.catColor(kind)
	boxStyle := lipgloss.NewStyle().
		Border(ui.BorderFor(m.cfg.Border)).
		Foreground(lipgloss.Color(col))
	return ui.Box(ui.BorderFor(m.cfg.Border), boxStyle,
		lipgloss.NewStyle().Foreground(lipgloss.Color(col)),
		m.catTitle(kind), label, right, padded)
}

func (m *Model) kv(key, val string) string {
	return m.th.Styles.Muted.Render(fmt.Sprintf("%-8s", key)) + val
}

func clamp01(v float64) float64 { return min(max(v, 0), 1) }

func clampInt(v, lo, hi int) int { return min(max(v, lo), hi) }
