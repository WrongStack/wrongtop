// Package sensors renders the sensors tab: rolling temperature graphs,
// fan speeds and battery state — everything the platform reports about
// its own hardware.
package sensors

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

// fanSamples is the history length of the per-fan sparklines.
const fanSamples = 12

// Model is the sensors tab.
type Model struct {
	cfg *config.Config
	th  *theme.Theme

	width, height int
	live          bool
	snap          collector.Snapshot

	tempHist map[string]*canvas.Graph // rolling graph per sensor name
	fanHist  map[string][]float64     // per-fan rpm history
	batGraph *canvas.Graph
}

// New builds the sensors tab.
func New(cfg *config.Config, th *theme.Theme) *Model {
	return &Model{
		cfg:      cfg,
		th:       th,
		tempHist: make(map[string]*canvas.Graph),
		fanHist:  make(map[string][]float64),
		batGraph: canvas.New(24, 2, th.Ramps.Bat.Colors()),
	}
}

// Title implements ui.Tab.
func (m *Model) Title() string { return ui.Icon("sensor", m.cfg.NerdFonts) + " SENSORS" }

// SetVisible implements ui.Tab. History keeps flowing while hidden so
// the graphs are never gapped; there is no per-snapshot rebuild to skip.
func (m *Model) SetVisible(bool) {}

// SetTheme implements ui.Tab.
func (m *Model) SetTheme(th *theme.Theme) {
	m.th = th
	for _, g := range m.tempHist {
		g.SetRamp(th.Ramps.CPU.Colors())
	}
	m.batGraph.SetRamp(th.Ramps.Bat.Colors())
}

// graphW sizes the rolling graphs from the terminal width.
func (m *Model) graphW() int { return clampInt(m.width-24, 16, 44) }

// SetSize implements ui.Tab.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	w := m.graphW()
	for _, g := range m.tempHist {
		g.Resize(w, 2)
	}
	m.batGraph.Resize(w, 2)
}

// Update implements ui.Tab.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case collector.SnapshotMsg:
		m.snap = msg.Snap
		m.live = true
		// Sensor and fan label churn (hwmon re-enumeration, remote
		// snapshots) must not grow the history maps without limit —
		// the same bound the io histories apply to device names.
		if len(m.tempHist) > 128 {
			m.tempHist = make(map[string]*canvas.Graph)
		}
		if len(m.fanHist) > 128 {
			m.fanHist = make(map[string][]float64)
		}
		w := m.graphW()
		for _, s := range m.snap.Sensors {
			g, ok := m.tempHist[s.Name]
			if !ok {
				g = canvas.New(w, 2, m.th.Ramps.CPU.Colors())
				m.tempHist[s.Name] = g
			}
			g.Push(clamp01(s.TempC / 100))
		}
		for i, f := range m.snap.Fans {
			name := fanName(f.Name, i)
			h := append(m.fanHist[name], f.RPM)
			if len(h) > fanSamples {
				h = h[len(h)-fanSamples:]
			}
			m.fanHist[name] = h
		}
		if b := m.snap.Battery; b != nil {
			m.batGraph.Push(clamp01(b.Percent / 100))
		}
		return nil
	}
	return nil
}

// View implements ui.Tab: one bordered section per hardware group, each
// dropped when the platform reports nothing for it. Graphs are the first
// thing dropped when the content outgrows a small terminal.
func (m *Model) View() string {
	if m.width < 1 {
		return ""
	}
	if !m.live {
		return ui.Box(ui.BorderFor(m.cfg.Border), m.th.Styles.Border, m.th.Styles.BorderChar,
			m.th.Styles.BorderTitle, m.Title(), "", m.th.Styles.Muted.Render("waiting for samples…"))
	}

	out := m.build(true)
	if strings.Count(out, "\n")+1 > m.height { // tight: drop the graphs
		out = m.build(false)
	}
	return out
}

// build renders the whole tab; withGraphs controls the rolling history
// blocks, so small terminals trade the curves for bare readings.
func (m *Model) build(withGraphs bool) string {
	var rows []string
	if len(m.snap.Sensors) > 0 {
		var body []string
		for i, s := range m.snap.Sensors {
			if i > 0 {
				body = append(body, "")
			}
			body = append(body, m.tempBlock(s, withGraphs)...)
		}
		rows = append(rows, m.section("sensor", "TEMPERATURES", body), "")
	}
	if len(m.snap.Fans) > 0 {
		var body []string
		for i, f := range m.snap.Fans {
			body = append(body, m.fanRow(f, i))
		}
		rows = append(rows, m.section("fan", "COOLING", body), "")
	}
	if b := m.snap.Battery; b != nil {
		rows = append(rows, m.section("bat", "POWER", m.batteryBlock(b, withGraphs)), "")
	}

	if len(rows) == 0 { // nothing reported anywhere
		empty := m.th.Styles.Muted.Render("no temperature, fan or battery sensors reported on this machine")
		return lipgloss.JoinVertical(lipgloss.Center, "", empty)
	}
	return strings.Join(rows, "\n")
}

// tempBlock renders one sensor: a header with the threshold-colored
// reading over its rolling graph.
func (m *Model) tempBlock(s collector.Sensor, withGraph bool) []string {
	style := m.th.Value(m.cfg.Thresholds.TempWarn, m.cfg.Thresholds.TempCrit, s.TempC)
	lines := []string{
		m.th.Styles.Muted.Render(fmt.Sprintf("%-16s", ui.Trunc(s.Name, 16))) +
			style.Render(fmt.Sprintf("%.1f°C", s.TempC)),
	}
	if g, ok := m.tempHist[s.Name]; ok && withGraph {
		lines = append(lines, strings.Split(g.View(), "\n")...)
	}
	return lines
}

// fanRow renders one fan: name, speed and its activity sparkline.
func (m *Model) fanRow(f collector.Fan, i int) string {
	spark := canvas.SparklineScaled(m.fanHist[fanName(f.Name, i)], m.th.Ramps.Load)
	return m.th.Styles.Muted.Render(fmt.Sprintf("%-16s", ui.Trunc(fanName(f.Name, i), 16))) +
		m.th.Styles.OK.Render(fmt.Sprintf("%6.0f", f.RPM)) +
		m.th.Styles.Muted.Render("rpm  ") + spark
}

// batteryBlock renders the charge level: header with state, a gradient
// meter and — when there is room — the rolling history.
func (m *Model) batteryBlock(b *collector.Battery, withGraph bool) []string {
	state := "on battery"
	if b.Charging {
		state = "charging " + ui.Icon("bolt", m.cfg.NerdFonts)
	}
	head := m.th.Styles.Muted.Render(fmt.Sprintf("%-16s", "BATTERY")) +
		m.th.Styles.FG.Render(fmt.Sprintf("%.0f%%", b.Percent)) +
		m.th.Styles.Muted.Render("  "+state)
	lines := []string{head,
		canvas.GradientBar(m.graphW(), b.Percent/100, m.th.Ramps.Bat, m.th.Styles.Track)}
	if withGraph {
		lines = append(lines, strings.Split(m.batGraph.View(), "\n")...)
	}
	return lines
}

// section wraps content in a titled, accent-colored box (the same chrome
// language as the dashboard's stacked panels).
func (m *Model) section(kind, title string, body []string) string {
	if len(body) == 0 {
		return ""
	}
	col := m.accent(kind)
	boxStyle := lipgloss.NewStyle().
		Border(ui.BorderFor(m.cfg.Border)).
		Foreground(lipgloss.Color(col))
	return ui.Box(ui.BorderFor(m.cfg.Border), boxStyle,
		lipgloss.NewStyle().Foreground(lipgloss.Color(col)),
		lipgloss.NewStyle().Foreground(lipgloss.Color(col)).Bold(true),
		ui.Icon(kind, m.cfg.NerdFonts)+" "+title, "", strings.Join(body, "\n"))
}

// accent picks the section color: sensors orange, cooling green, power cyan.
func (m *Model) accent(kind string) string {
	pal := m.th.Palette
	switch kind {
	case "sensor":
		return pal.Orange
	case "fan":
		return pal.Green
	case "bat":
		return pal.Cyan
	}
	return pal.FG
}

// fanName derives a stable history key for a fan reading.
func fanName(name string, i int) string {
	if name != "" {
		return name
	}
	return fmt.Sprintf("F%d", i)
}

func clamp01(v float64) float64 { return min(max(v, 0), 1) }

func clampInt(v, lo, hi int) int { return min(max(v, lo), hi) }
