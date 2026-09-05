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

	// value ramps for the gradient meters, derived from the theme
	cpuRamp canvas.Ramp
	memRamp canvas.Ramp
	ioRamp  canvas.Ramp
	batRamp canvas.Ramp
	rxRamp  canvas.Ramp // download share of the mixed meters
	txRamp  canvas.Ramp // upload share of the mixed meters
}

// Density presets cycle with p and start from config layout.
const (
	densityFull = iota
	densityCompact
	densityMinimal
)

// New builds the dashboard tab.
func New(cfg *config.Config, th *theme.Theme) *Model {
	density := densityFull
	switch cfg.Layout {
	case "compact":
		density = densityCompact
	case "minimal":
		density = densityMinimal
	}
	m := &Model{
		cfg:       cfg,
		th:        th,
		density:   density,
		ioHist:    make(map[string][]float64),
		cpuGraph:  canvas.New(60, 3, ramp(th, th.Palette.Green, th.Palette.Yellow, th.Palette.Orange, th.Palette.Red)),
		memGraph:  canvas.New(34, 2, ramp(th, th.Palette.Blue, th.Palette.Purple, th.Palette.Red)),
		swapGraph: canvas.New(34, 1, ramp(th, th.Palette.Purple, th.Palette.Red)),
		rxGraph:   canvas.New(netCol-2, 2, ramp(th, th.Palette.Green, th.Palette.Cyan)),
		txGraph:   canvas.New(netCol-2, 2, ramp(th, th.Palette.Blue, th.Palette.Cyan)),
		loadGraph: canvas.New(34, 2, ramp(th, th.Palette.Cyan, th.Palette.Green)),
	}
	m.applyRamps(th)
	return m
}

// applyRamps derives the gradient meters from the palette.
func (m *Model) applyRamps(th *theme.Theme) {
	p := th.Palette
	m.cpuRamp = canvas.Ramp{p.Green, p.Yellow, p.Orange, p.Red}
	m.memRamp = canvas.Ramp{p.Blue, p.Purple, p.Red}
	m.ioRamp = canvas.Ramp{p.Cyan, p.Green, p.Yellow, p.Red}
	m.batRamp = canvas.Ramp{p.Red, p.Orange, p.Green}
	m.rxRamp = canvas.Ramp{p.Green, p.Cyan}
	m.txRamp = canvas.Ramp{p.Blue, p.Cyan}
}

// Title implements ui.Tab.
func (m *Model) Title() string { return "⌂ DASHBOARD" }

// SetTheme implements ui.Tab; graph ramps derive from the palette so
// they must be rebuilt alongside the styles.
func (m *Model) SetTheme(th *theme.Theme) {
	m.th = th
	m.viewCache = ""
	m.applyRamps(th)
	m.cpuGraph.SetRamp(ramp(th, th.Palette.Green, th.Palette.Yellow, th.Palette.Orange, th.Palette.Red))
	m.memGraph.SetRamp(ramp(th, th.Palette.Blue, th.Palette.Purple, th.Palette.Red))
	m.swapGraph.SetRamp(ramp(th, th.Palette.Purple, th.Palette.Red))
	m.rxGraph.SetRamp(ramp(th, th.Palette.Green, th.Palette.Cyan))
	m.txGraph.SetRamp(ramp(th, th.Palette.Blue, th.Palette.Cyan))
	m.loadGraph.SetRamp(ramp(th, th.Palette.Cyan, th.Palette.Green))
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
	cpuH := 3
	if m.density == densityFull && m.class() == layoutGrid {
		// leave room for the big-digit hero beside a hero-height graph:
		// up to three digits (11 cells) plus the two-column gap
		cpuW = max(20, width-hostCol-2-13)
		cpuH = canvas.BigNumberHeight
	}
	memW := max(14, hostCol-2)
	rxW := max(10, netCol-2)
	if m.class() == layoutNarrow {
		cpuW = max(16, width-6)
		memW = max(12, width/2-8)
		rxW = max(12, width-6)
	}
	m.cpuGraph.Resize(cpuW, cpuH)
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
// sparklines.
func (m *Model) recordIO() {
	for _, d := range m.snap.DiskIOs {
		h := append(m.ioHist[d.Name], d.ReadBytes+d.WriteBytes)
		if len(h) > ioSparkSamples {
			h = h[len(h)-ioSparkSamples:]
		}
		m.ioHist[d.Name] = h
	}
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
			m.th.Styles.BorderTitle, "WRONGTOP", m.th.Styles.Muted.Render("waiting for samples…"))
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

	title := func(color string) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true)
	}
	// per-category border colors, btop-style: each panel owns its frame
	borders := func(color string) *lipgloss.Style {
		st := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		return &st
	}
	pal := m.th.Palette
	// divider columns at x=hostW and x=hostW+netW
	hostW := hostCol
	netW := netCol
	cpuInner := w - hostW - 2

	panels := []ui.Panel{
		{X: 0, Y: 0, W: hostW + 1, H: a, Title: "⌂ HOST", TitleStyle: title(pal.Blue),
			Border: borders(pal.Blue), Lines: m.hostView(hostW-2, a-2)},
		{X: hostW, Y: 0, W: w - hostW, H: a, Title: "⚡ CPU", TitleStyle: title(pal.Green),
			Border: borders(pal.Green), Lines: m.cpuView(cpuInner, a-2)},
	}
	if m.density == densityMinimal {
		return panels
	}

	panels = append(panels,
		ui.Panel{X: 0, Y: a - 1, W: hostW + 1, H: b, Title: "▦ MEMORY", TitleStyle: title(pal.Purple),
			Border: borders(pal.Purple), Lines: m.memView(hostW - 2)},
		ui.Panel{X: hostW, Y: a - 1, W: netW + 1, H: b, Title: "⇅ NETWORK", TitleStyle: title(pal.Cyan),
			Border: borders(pal.Cyan), Lines: m.netView(max(0, b-7))},
		ui.Panel{X: hostW + netW, Y: a - 1, W: w - hostW - netW, H: b, Title: "▤ DISKS", TitleStyle: title(pal.Orange),
			Border: borders(pal.Orange), Lines: m.diskView(w-hostW-netW-2, max(1, b-3))},
	)
	if c == 0 {
		return panels
	}

	procY, procH := a+b-2, c
	if gpus := len(m.snap.GPUs); gpus > 0 && c >= 8 {
		gh := min(gpus+3, 6) // up to four adapters + borders
		panels = append(panels, ui.Panel{X: 0, Y: procY, W: w, H: gh, Title: "◆ GPUS",
			TitleStyle: title(pal.Red), Border: borders(pal.Red), Lines: m.gpuLines(w-2, gh-2)})
		procY += gh - 1 // share the border row
		procH -= gh - 1
	}
	panels = append(panels, ui.Panel{X: 0, Y: procY, W: w, H: procH, Title: "☰ PROCESSES",
		TitleStyle: title(pal.FG), Border: borders(pal.FG), Lines: m.procView(w-2, max(2, procH-3))})
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
			m.box(w-2, "HOST", strings.Join(m.hostView(w-4, 99), "\n")), "",
			m.box(w-2, "CPU", strings.Join(m.cpuView(cpuInner, coreRows+4), "\n")), "",
		)
	} else {
		rows = append(rows,
			lipgloss.JoinHorizontal(lipgloss.Top,
				m.box(hostCol-2, "HOST", padLines(m.hostView(hostCol-4, coreRows+4), coreRows+4)),
				" ",
				m.box(cpuInner, "CPU", padLines(m.cpuView(cpuInner, coreRows+4), coreRows+4)),
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
		if gpus := len(m.snap.GPUs); gpus > 0 && procRows >= 5 {
			gpuRows := min(3, gpus)
			rows = append(rows,
				m.box(w-2, "GPUS", strings.Join(m.gpuLines(w-6, gpuRows), "\n")), "")
			procRows -= gpuRows + 1
		}
		rows = append(rows, m.box(w-2, "PROCESSES", strings.Join(m.procView(w-6, procRows), "\n")))
	}
	return rows
}

// box renders a titled border around content padded to exactly innerW
// cells so grid columns line up.
func (m *Model) box(innerW int, title, content string) string {
	padded := lipgloss.NewStyle().Width(innerW).Render(content)
	return ui.Box(ui.BorderFor(m.cfg.Border), m.th.Styles.Border, m.th.Styles.BorderChar,
		m.th.Styles.BorderTitle, title, padded)
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
