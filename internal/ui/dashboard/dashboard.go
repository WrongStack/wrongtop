// Package dashboard renders the wrongtop overview tab: host identity,
// CPU graph, memory and swap.
package dashboard

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ersinkoc/wrongtop/internal/collector"
	"github.com/ersinkoc/wrongtop/internal/config"
	"github.com/ersinkoc/wrongtop/internal/format"
	"github.com/ersinkoc/wrongtop/internal/theme"
	"github.com/ersinkoc/wrongtop/internal/ui"
	"github.com/ersinkoc/wrongtop/internal/ui/canvas"
)

// Model is the dashboard tab.
type Model struct {
	cfg *config.Config
	th  *theme.Theme

	width, height int

	cpuGraph *canvas.Graph
	memGraph *canvas.Graph

	host collector.Host
	cpu  collector.CPU
	mem  collector.Mem
	live bool // at least one snapshot received
}

// New builds the dashboard tab.
func New(cfg *config.Config, th *theme.Theme) *Model {
	return &Model{
		cfg:      cfg,
		th:       th,
		cpuGraph: canvas.New(60, 3, ramp(th, th.Palette.Green, th.Palette.Yellow, th.Palette.Orange, th.Palette.Red)),
		memGraph: canvas.New(26, 2, ramp(th, th.Palette.Blue, th.Palette.Purple, th.Palette.Red)),
	}
}

// Title implements ui.Tab.
func (m *Model) Title() string { return "DASHBOARD" }

// SetSize implements ui.Tab.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	if width < 1 || height < 1 {
		return
	}
	hostW := hostBoxWidth
	if width < wideLayoutMin {
		hostW = 0 // host box stacks above; CPU uses full width
	}
	cpuW := max(20, width-hostW-4)
	m.cpuGraph.Resize(cpuW, 3)
	memW := max(16, width/2-4)
	m.memGraph.Resize(memW, 2)
}

// Update implements ui.Tab.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if snap, ok := msg.(collector.SnapshotMsg); ok {
		m.host = snap.Snap.Host
		m.cpu = snap.Snap.CPU
		m.mem = snap.Snap.Mem
		m.cpuGraph.Push(clamp01(m.cpu.Percent / 100))
		m.memGraph.Push(clamp01(m.mem.Percent / 100))
		m.live = true
	}
	return nil
}

// View implements ui.Tab.
func (m *Model) View() string {
	if m.width < 1 {
		return ""
	}
	hostBox := ui.Box(lipgloss.RoundedBorder(), m.th.Styles.Border, m.th.Styles.BorderChar, m.th.Styles.BorderTitle,
		"HOST", m.hostView())
	cpuBox := ui.Box(lipgloss.RoundedBorder(), m.th.Styles.Border, m.th.Styles.BorderChar, m.th.Styles.BorderTitle,
		"CPU", m.cpuView())
	memBox := ui.Box(lipgloss.RoundedBorder(), m.th.Styles.Border, m.th.Styles.BorderChar, m.th.Styles.BorderTitle,
		"MEM", m.memView(m.mem.Total, m.mem.Used, m.mem.Percent, true))
	swapBox := ui.Box(lipgloss.RoundedBorder(), m.th.Styles.Border, m.th.Styles.BorderChar, m.th.Styles.BorderTitle,
		"SWAP", m.memView(m.mem.SwapTotal, m.mem.SwapUsed, m.mem.SwapPercent, false))

	var top string
	if m.width < wideLayoutMin {
		top = lipgloss.JoinVertical(lipgloss.Left, hostBox, cpuBox)
	} else {
		top = lipgloss.JoinHorizontal(lipgloss.Top, hostBox, " ", cpuBox)
	}
	return lipgloss.JoinVertical(lipgloss.Left, top, " ",
		lipgloss.JoinHorizontal(lipgloss.Top, memBox, " ", swapBox))
}

func (m *Model) hostView() string {
	if !m.live {
		return m.th.Styles.Muted.Render("waiting for samples…")
	}
	lines := []string{
		m.kv("NAME", m.host.Hostname),
		m.kv("SYS", strings.TrimSpace(m.host.Platform+" "+m.host.Arch)),
		m.kv("KERNEL", m.host.Kernel),
		m.kv("UPTIME", format.Uptime(m.host.Uptime)),
		m.kv("LOAD", fmt.Sprintf("%.2f %.2f %.2f", m.host.Load[0], m.host.Load[1], m.host.Load[2])),
		m.kv("PROCS", strconv.Itoa(m.host.Procs)),
	}
	return strings.Join(lines, "\n")
}

func (m *Model) cpuView() string {
	if !m.live {
		return m.th.Styles.Muted.Render("waiting for samples…")
	}
	pct := m.th.Value(m.cfg.Thresholds.CPUWarn, m.cfg.Thresholds.CPUCrit, m.cpu.Percent).
		Render(fmt.Sprintf("%.1f%%", m.cpu.Percent))
	head := m.th.Styles.Muted.Render("TOTAL ")+pct+
		m.th.Styles.Muted.Render(fmt.Sprintf("  ·  %d cores", len(m.cpu.Cores)))
	body := strings.Join([]string{head, m.cpuGraph.View()}, "\n")

	if rows := m.perCoreView(); rows != "" {
		body += "\n" + rows
	}
	return body
}

// perCoreView renders mini bars, up to perCoreColumns per row, bounded by
// the space the content box has left.
func (m *Model) perCoreView() string {
	if len(m.cpu.Cores) == 0 {
		return ""
	}
	maxRows := (m.height - 8) / 1 // top boxes + mem row occupy the rest
	if maxRows < 1 {
		maxRows = 1
	}
	perRow := 4
	if m.width >= wideLayoutMin {
		perRow = max(2, (m.width-hostBoxWidth)/14)
	}

	shown := len(m.cpu.Cores)
	rows := (shown + perRow - 1) / perRow
	hidden := 0
	if rows > maxRows {
		shown = maxRows * perRow
		rows = maxRows
		hidden = len(m.cpu.Cores) - shown
	}

	var out []string
	for r := 0; r < rows; r++ {
		var cells []string
		for c := 0; c < perRow; c++ {
			i := r*perRow + c
			if i >= shown {
				break
			}
			cells = append(cells, m.coreBar(i, m.cpu.Cores[i]))
		}
		out = append(out, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}
	if hidden > 0 {
		out = append(out, m.th.Styles.Muted.Render(fmt.Sprintf("… +%d more cores", hidden)))
	}
	return strings.Join(out, "\n")
}

func (m *Model) coreBar(i int, pct float64) string {
	label := m.th.Styles.Muted.Render(fmt.Sprintf("c%-2d", i))
	bar := canvas.Bar(10, pct/100,
		m.th.Value(m.cfg.Thresholds.CPUWarn, m.cfg.Thresholds.CPUCrit, pct),
		m.th.Styles.Muted,
	)
	val := m.th.Styles.Muted.Render(fmt.Sprintf("%3.0f%%", pct))
	return label + bar + val
}

func (m *Model) memView(total, used uint64, pct float64, withGraph bool) string {
	if !m.live {
		return m.th.Styles.Muted.Render("waiting for samples…")
	}
	pctS := m.th.Value(m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit, pct).
		Render(fmt.Sprintf("%.1f%%", pct))
	head := pctS + m.th.Styles.Muted.Render(
		fmt.Sprintf("  %s / %s", format.Bytes(used), format.Bytes(total)))
	bar := canvas.Bar(24, pct/100,
		m.th.Value(m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit, pct),
		m.th.Styles.Muted,
	)
	lines := []string{head, bar}
	if withGraph {
		lines = append(lines, m.memGraph.View())
	}
	return strings.Join(lines, "\n")
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

// layout knobs.
const (
	hostBoxWidth  = 34
	wideLayoutMin = 80
)
