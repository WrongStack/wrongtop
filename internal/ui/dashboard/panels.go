package dashboard

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/ersinkoc/wrongtop/internal/collector"
	"github.com/ersinkoc/wrongtop/internal/config"
	"github.com/ersinkoc/wrongtop/internal/format"
	"github.com/ersinkoc/wrongtop/internal/ui/canvas"
)

// hostView renders the identity panel: one kv row per fact, plus
// temperature, battery and fan rows when the platform reports them, and
// a load-average graph when the row budget allows. The graph trails so
// tight budgets drop it first.
func (m *Model) hostView(maxRows int) []string {
	h := m.snap.Host
	loadStyle := m.th.Styles.Muted
	cores := float64(len(m.snap.CPU.Cores))
	loadMeter := ""
	if cores > 0 {
		loadStyle = m.th.Value(cores*3/4, cores, h.Load[0])
		// a small meter scaled to the core count, btop-style
		loadMeter = " " + canvas.GradientBar(8, clamp01(h.Load[0]/cores), m.cpuRamp, m.th.Styles.Muted)
	}
	lines := []string{
		m.kv("NAME", h.Hostname),
		m.kv("SYS", strings.TrimSpace(h.Platform+" "+h.Arch)),
		m.kv("KERNEL", trunc(h.Kernel, 18)),
		m.kv("UPTIME", format.Uptime(h.Uptime)),
		m.kv("LOAD", loadStyle.Render(fmt.Sprintf("%.2f %.2f %.2f", h.Load[0], h.Load[1], h.Load[2]))+loadMeter),
		m.kv("PROCS", strconv.Itoa(h.Procs)),
	}
	if h.Users > 0 {
		lines = append(lines, m.kv("USERS", strconv.Itoa(h.Users)))
	}
	if s := m.hotSensor(); s != nil {
		style := m.th.Value(m.cfg.Thresholds.TempWarn, m.cfg.Thresholds.TempCrit, s.TempC)
		lines = append(lines, m.kv("TEMP", style.Render(fmt.Sprintf("%.0f°C", s.TempC))+
			m.th.Styles.Muted.Render(" "+trunc(s.Name, 12))))
	}
	if b := m.snap.Battery; b != nil {
		state := "on battery"
		if b.Charging {
			state = "charging ⚡"
		}
		meter := canvas.GradientBar(8, b.Percent/100, m.batRamp, m.th.Styles.Muted)
		lines = append(lines, m.kv("BATTERY",
			fmt.Sprintf("%.0f%% ", b.Percent)+meter+m.th.Styles.Muted.Render(" "+state)))
	}
	if len(m.snap.Fans) > 0 {
		fans := m.snap.Fans[:min(len(m.snap.Fans), 2)]
		vals := make([]string, len(fans))
		for i, f := range fans {
			vals[i] = fmt.Sprintf("%.0frpm", f.RPM)
		}
		row := strings.Join(vals, " · ")
		if extra := len(m.snap.Fans) - len(fans); extra > 0 {
			row += m.th.Styles.Muted.Render(fmt.Sprintf("  +%d", extra))
		}
		lines = append(lines, m.kv("FANS", row))
	}
	// the load graph trails: tight budgets truncate it first
	if maxRows-len(lines) >= 2 {
		lines = append(lines, strings.Split(m.loadGraph.View(), "\n")...)
	}
	return lines
}

// cpuView renders the total gauge and — on wide panels — the btop-style
// hero: big block digits beside the scrolling graph, then a full-width
// gradient meter, sensors and per-core bars. maxLines bounds the output.
func (m *Model) cpuView(innerW, maxLines int) []string {
	c := m.snap.CPU
	pct := m.th.Value(m.cfg.Thresholds.CPUWarn, m.cfg.Thresholds.CPUCrit, c.Percent).
		Render(fmt.Sprintf("%.1f%%", c.Percent))
	head := m.th.Styles.Muted.Render("TOTAL ") + pct
	if c.FreqMHz > 0 {
		head += m.th.Styles.Muted.Render(fmt.Sprintf("  %.1fGHz", c.FreqMHz/1000))
	}
	if s := m.hotSensor(); s != nil {
		style := m.th.Value(m.cfg.Thresholds.TempWarn, m.cfg.Thresholds.TempCrit, s.TempC)
		head += m.th.Styles.Muted.Render("  ") + style.Render(fmt.Sprintf("%.0f°C", s.TempC))
	}

	lines := []string{head}
	if sl := m.sensorsLine(innerW); sl != "" {
		lines = append(lines, sl)
	}
	if innerW > 20 { // full-width gradient meter, btop-style
		lines = append(lines, canvas.GradientBar(innerW, c.Percent/100, m.cpuRamp, m.th.Styles.Muted))
	}

	graph := strings.Split(m.cpuGraph.View(), "\n")
	if hero := m.heroWorth(innerW, maxLines); hero {
		// big digits left, graph right: the headline moment of the tab
		big := canvas.BigNumber(int(c.Percent+0.5), m.cpuRamp)
		gap := strings.Repeat(" ", 2)
		rows := make([]string, 4)
		for i := range big {
			g := ""
			if i > 0 && i-1 < len(graph) {
				g = graph[i-1]
			}
			rows[i] = big[i] + gap + g
		}
		lines = append(lines, rows...)
	} else {
		lines = append(lines, graph...)
	}

	graphH := len(lines)
	if m.density < densityCompact {
		if coreLines := m.perCoreView(innerW, maxLines-graphH); coreLines != "" {
			lines = append(lines, strings.Split(coreLines, "\n")...)
		}
	}
	return lines
}

// heroWorth reports whether the panel is wide and tall enough for the
// big-digit hero composition.
func (m *Model) heroWorth(innerW, maxLines int) bool {
	return m.density == densityFull && innerW >= 56 && maxLines >= 8
}

// perCoreView renders mini bars, up to perCoreColumns per row, bounded by
// maxRows.
func (m *Model) perCoreView(innerW, maxRows int) string {
	if len(m.snap.CPU.Cores) == 0 || maxRows < 1 {
		return ""
	}
	perRow := clampInt(innerW/19, 2, 6)

	shown := len(m.snap.CPU.Cores)
	rows := (shown + perRow - 1) / perRow
	hidden := 0
	if rows > maxRows {
		shown = maxRows * perRow
		rows = maxRows
		hidden = len(m.snap.CPU.Cores) - shown
	}

	out := make([]string, 0, rows+1)
	for r := 0; r < rows; r++ {
		var cells []string
		for c := 0; c < perRow; c++ {
			i := r*perRow + c
			if i >= shown {
				break
			}
			cells = append(cells, m.coreBar(i, m.snap.CPU.Cores[i]))
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
	bar := canvas.GradientBar(10, pct/100, m.cpuRamp, m.th.Styles.Muted)
	val := m.th.Styles.Muted.Render(fmt.Sprintf("%3.0f%%", pct))
	return label + bar + val
}

// memView renders gradient meters for RAM, swap and zram — with a
// big-digit hero on wide panels; the history graphs trail so they are
// the first thing dropped on short screens.
func (m *Model) memView(innerW int) []string {
	mem := m.snap.Mem
	barW := clampInt(innerW-14, 12, 28)

	right := []string{
		m.th.Styles.Muted.Render(fmt.Sprintf("RAM   %s / %s", format.Bytes(mem.Used), format.Bytes(mem.Total))),
		canvas.GradientBar(barW, mem.Percent/100, m.memRamp, m.th.Styles.Muted),
	}

	if mem.SwapTotal > 0 {
		right = append(right,
			m.valuePct(mem.SwapPercent, m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit)+
				m.th.Styles.Muted.Render(fmt.Sprintf("  SWAP  %s / %s",
					format.Bytes(mem.SwapUsed), format.Bytes(mem.SwapTotal))),
			canvas.GradientBar(barW, mem.SwapPercent/100, m.memRamp, m.th.Styles.Muted),
		)
	} else {
		right = append(right, m.th.Styles.Muted.Render("SWAP —"))
	}

	if mem.ZramTotal > 0 { // linux compressed swap device
		pct := float64(mem.ZramUsed) / float64(mem.ZramTotal) * 100
		right = append(right,
			m.valuePct(pct, m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit)+
				m.th.Styles.Muted.Render(fmt.Sprintf("  ZRAM  %s / %s",
					format.Bytes(mem.ZramUsed), format.Bytes(mem.ZramTotal))),
			canvas.GradientBar(barW, pct/100, m.ioRamp, m.th.Styles.Muted),
		)
	}

	var lines []string
	if heroWorth := innerW >= 26 && m.density != densityMinimal; heroWorth {
		// big percent left, meters right
		big := canvas.BigNumber(int(mem.Percent+0.5), m.memRamp)
		gap := strings.Repeat(" ", 2)
		for i := 0; i < len(big) || i < len(right); i++ {
			l, r := "", ""
			if i < len(big) {
				l = big[i]
			}
			if i < len(right) {
				r = right[i]
			}
			if i == 0 && l != "" { // align the meters under the digits
				l += gap
			} else if l != "" {
				l += gap
			}
			lines = append(lines, l+r)
		}
	} else {
		lines = append(lines,
			m.valuePct(mem.Percent, m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit)+
				m.th.Styles.Muted.Render(fmt.Sprintf("  RAM   %s / %s",
					format.Bytes(mem.Used), format.Bytes(mem.Total))))
		lines = append(lines, right[1])
	}

	if m.density < densityCompact { // history graphs trail, dropped first
		lines = append(lines, strings.Split(m.memGraph.View(), "\n")...)
		if mem.SwapTotal > 0 {
			lines = append(lines, strings.Split(m.swapGraph.View(), "\n")...)
		}
	}
	return lines
}

// netView renders total down/up graphs plus the busiest interfaces.
func (m *Model) netView(maxIface int) []string {
	rx, tx := totalRates(m.snap.Nets)
	head := m.th.Styles.OK.Render("↓ "+format.Rate(rx)) +
		m.th.Styles.Muted.Render("  ") +
		m.th.Styles.Title.Render("↑ "+format.Rate(tx))

	lines := []string{head}
	lines = append(lines, strings.Split(m.rxGraph.View(), "\n")...)
	lines = append(lines, strings.Split(m.txGraph.View(), "\n")...)

	ifaces := make([]collector.NetIface, len(m.snap.Nets))
	copy(ifaces, m.snap.Nets)
	sort.Slice(ifaces, func(i, j int) bool {
		return ifaces[i].RxRate+ifaces[i].TxRate > ifaces[j].RxRate+ifaces[j].TxRate
	})
	for _, n := range ifaces[:min(len(ifaces), max(0, maxIface))] {
		spark := canvas.Sparkline(m.netHist[n.Name], m.ioRamp)
		total := n.RxRate + n.TxRate
		style := m.th.Styles.OK // green for download-dominated
		if n.TxRate > n.RxRate {
			style = m.th.Styles.Title // upload-dominated
		}
		lines = append(lines, m.th.Styles.Muted.Render(fmt.Sprintf("%-8s", trunc(n.Name, 8)))+
			spark+" "+style.Render(shortRate(total)))
	}
	return lines
}

// diskView renders an aggregate I/O line plus usage bars per mount.
func (m *Model) diskView(innerW, maxRows int) []string {
	var rb, wb float64
	for _, io := range m.snap.DiskIOs {
		rb += io.ReadBytes
		wb += io.WriteBytes
	}
	lines := []string{m.th.Styles.Muted.Render("I/O ") +
		fmt.Sprintf("R %s · W %s", format.Rate(rb), format.Rate(wb))}

	disks := make([]collector.Disk, len(m.snap.Disks))
	copy(disks, m.snap.Disks)
	sort.Slice(disks, func(i, j int) bool { return disks[i].Percent > disks[j].Percent })

	barW := clampInt(innerW-34, 6, 16)
	for _, d := range disks[:min(len(disks), max(0, maxRows))] {
		mount := trunc(filepath.Base(d.Mountpoint), 9)
		name := m.th.Styles.Muted.Render(fmt.Sprintf("%-9s", mount))
		bar := canvas.GradientBar(barW, d.Percent/100, m.memRamp, m.th.Styles.Muted)
		spark := canvas.Sparkline(m.ioHist[filepath.Base(d.Device)], m.ioRamp)
		pct := m.valuePct(d.Percent, m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit)
		lines = append(lines, name+bar+pct+spark)
	}
	if len(disks) > maxRows {
		lines = append(lines, m.th.Styles.Muted.Render(fmt.Sprintf("… +%d more", len(disks)-maxRows)))
	}
	return lines
}

// procView renders the busiest processes, CPU first. maxRows bounds the
// data rows; a summary line always leads.
func (m *Model) procView(innerW, maxRows int) []string {
	procs := make([]collector.Proc, len(m.snap.Procs))
	copy(procs, m.snap.Procs)
	sort.Slice(procs, func(i, j int) bool {
		if procs[i].CPU != procs[j].CPU {
			return procs[i].CPU > procs[j].CPU
		}
		return procs[i].Mem > procs[j].Mem // tiebreak: memory
	})

	summary := m.th.Styles.Muted.Render(fmt.Sprintf("%d procs · sorted by cpu · top %d",
		len(procs), min(len(procs), max(0, maxRows))))
	lines := []string{summary}

	nameW := max(10, innerW-38)
	for _, p := range procs[:min(len(procs), max(0, maxRows))] {
		// CPU colored along the value ramp instead of three flat states
		cpuColor := lipgloss.Color(m.cpuRamp.At(min(p.CPU, 100) / 100))
		cpuStyle := lipgloss.NewStyle().Foreground(cpuColor)
		lines = append(lines,
			m.th.Styles.Muted.Render(fmt.Sprintf("%7d ", p.PID))+
				fmt.Sprintf("%-9s", trunc(p.User, 9))+
				m.th.Styles.Muted.Render(fmt.Sprintf("%5.1f ", p.Mem))+
				cpuStyle.Render(fmt.Sprintf("%5.1f ", p.CPU))+
				fmt.Sprintf("%8s ", format.Bytes(p.RSS))+
				trunc(p.Name, nameW),
		)
	}
	return lines
}

// Alert is one threshold crossing. Key identifies the metric so the
// app-level alert history can track start and end of each condition.
type Alert struct {
	Key  string
	Text string
	Crit bool
}

// EvaluateAlerts collects threshold crossings, glances-style. Critical
// findings sort before warnings so the strip leads with the worst. A
// nil snapshot (never received one) yields no alerts.
func EvaluateAlerts(cfg *config.Config, snap collector.Snapshot) []Alert {
	if snap.Time.IsZero() {
		return nil
	}
	t := cfg.Thresholds
	var out []Alert

	pct := func(key, label string, warn, crit, v float64) {
		switch {
		case v >= crit:
			out = append(out, Alert{key, fmt.Sprintf("%s %.0f%%", label, v), true})
		case v >= warn:
			out = append(out, Alert{key, fmt.Sprintf("%s %.0f%%", label, v), false})
		}
	}
	pct("cpu", "CPU", t.CPUWarn, t.CPUCrit, snap.CPU.Percent)
	pct("mem", "MEM", t.MemWarn, t.MemCrit, snap.Mem.Percent)
	pct("swap", "SWAP", t.MemWarn, t.MemCrit, snap.Mem.SwapPercent)

	if s := hottestSensor(snap); s != nil {
		key := "temp:" + s.Name
		text := fmt.Sprintf("TEMP %s %.0f°C", s.Name, s.TempC)
		switch {
		case s.TempC >= t.TempCrit:
			out = append(out, Alert{key, text, true})
		case s.TempC >= t.TempWarn:
			out = append(out, Alert{key, text, false})
		}
	}

	var worst *collector.Disk
	for i := range snap.Disks {
		if worst == nil || snap.Disks[i].Percent > worst.Percent {
			worst = &snap.Disks[i]
		}
	}
	if worst != nil {
		pct("disk:"+worst.Mountpoint, "DISK "+filepath.Base(worst.Mountpoint),
			t.MemWarn, t.MemCrit, worst.Percent)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Crit && !out[j].Crit })
	return out
}

// alerts maps the shared evaluation onto the dashboard strip.
func (m *Model) alerts() []Alert {
	if !m.live {
		return nil
	}
	return EvaluateAlerts(m.cfg, m.snap)
}

func (m *Model) alertsView() string {
	al := m.alerts()
	if len(al) == 0 {
		return ""
	}
	pal := m.th.Palette
	chips := make([]string, len(al))
	for i, a := range al {
		bg := pal.Yellow
		if a.Crit {
			bg = pal.Red
		}
		chips[i] = lipgloss.NewStyle().Background(lipgloss.Color(bg)).
			Foreground(lipgloss.Color(pal.BG)).Bold(true).
			Padding(0, 1).Render("⚠ " + a.Text)
	}
	return strings.Join(chips, " ")
}

// sensorsLine renders every reported temperature on one line, hottest
// first, each value threshold-colored. The hottest reading already shows
// in the panel head and the host panel, so the line only appears when a
// second sensor exists. Width-bounded to the panel.
func (m *Model) sensorsLine(maxW int) string {
	if len(m.snap.Sensors) < 2 {
		return ""
	}
	line := m.th.Styles.Muted.Render("SENS ")
	for i, s := range m.snap.Sensors {
		style := m.th.Value(m.cfg.Thresholds.TempWarn, m.cfg.Thresholds.TempCrit, s.TempC)
		cell := style.Render(fmt.Sprintf("%.0f°", s.TempC)) +
			m.th.Styles.Muted.Render(" "+trunc(s.Name, 10))
		sep := ""
		if i > 0 {
			sep = m.th.Styles.Muted.Render(" · ")
		}
		if lipgloss.Width(line)+lipgloss.Width(sep)+lipgloss.Width(cell) > maxW {
			break
		}
		line += sep + cell
	}
	return line
}

// gpuLines renders one line per adapter: name, utilization bar, VRAM and
// temperature. The first line is the panel head.
func (m *Model) gpuLines(innerW, maxLines int) []string {
	gpus := m.snap.GPUs
	if len(gpus) == 0 || maxLines < 1 {
		return nil
	}
	lines := []string{m.th.Styles.Muted.Render(fmt.Sprintf("%d adapters", len(gpus)))}
	for _, g := range gpus[:min(len(gpus), max(0, maxLines-1))] {
		barW := clampInt(innerW-42, 6, 20)
		bar := canvas.GradientBar(barW, clamp01(g.Util/100), m.cpuRamp, m.th.Styles.Muted)
		mem := ""
		if g.MemTotal > 0 {
			mem = fmt.Sprintf(" %s/%s", format.Bytes(g.MemUsed), format.Bytes(g.MemTotal))
		}
		temp := m.th.Value(m.cfg.Thresholds.TempWarn, m.cfg.Thresholds.TempCrit, g.TempC).
			Render(fmt.Sprintf("%.0f°", g.TempC))
		lines = append(lines,
			m.th.Styles.Muted.Render(fmt.Sprintf("GPU%d ", g.Index))+
				trunc(g.Name, 24)+" "+bar+fmt.Sprintf("%4.0f%%", g.Util)+mem+" "+temp)
	}
	return lines
}

// hottestSensor returns the hottest sensor of a snapshot (the list is
// sorted hottest-first by the collector).
func hottestSensor(snap collector.Snapshot) *collector.Sensor {
	if len(snap.Sensors) == 0 {
		return nil
	}
	return &snap.Sensors[0]
}

// hotSensor returns the hottest reported sensor, if any.
func (m *Model) hotSensor() *collector.Sensor {
	return hottestSensor(m.snap)
}

// valuePct renders a percentage with threshold coloring.
func (m *Model) valuePct(pct, warn, crit float64) string {
	return m.th.Value(warn, crit, pct).Render(fmt.Sprintf("%.1f%%", pct))
}

// totalRates sums rx/tx byte rates across interfaces.
func totalRates(nets []collector.NetIface) (rx, tx float64) {
	for _, n := range nets {
		rx += n.RxRate
		tx += n.TxRate
	}
	return rx, tx
}

// shortRate renders a rate without the "/s" suffix for dense columns.
func shortRate(bps float64) string {
	return strings.TrimSuffix(format.Rate(bps), "/s")
}

// ifaceBudget is how many interface rows the network panel shows.
func ifaceBudget(h int) int { return clampInt((h-16)/5, 1, 4) }

// trunc shortens s to at most w-1 runes plus an ellipsis, cutting at
// rune boundaries so multi-byte names stay valid UTF-8.
func trunc(s string, w int) string {
	if len(s) <= w {
		return s
	}
	if w < 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w { // long in bytes, short in runes: nothing to drop
		return s
	}
	return string(r[:w-1]) + "…"
}

// padLines pads (or truncates) content to exactly n lines so grid boxes
// line up horizontally.
func padLines(lines []string, n int) string {
	split := strings.Split(strings.Join(lines, "\n"), "\n")
	if len(split) > n && n >= 0 {
		split = split[:n]
	}
	for len(split) < n {
		split = append(split, "")
	}
	return strings.Join(split, "\n")
}
