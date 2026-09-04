package dashboard

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/ersinkoc/wrongtop/internal/collector"
	"github.com/ersinkoc/wrongtop/internal/format"
	"github.com/ersinkoc/wrongtop/internal/ui/canvas"
)

// hostView renders the identity panel: one kv row per fact, plus
// temperature and battery rows when the platform reports them.
func (m *Model) hostView() []string {
	h := m.snap.Host
	loadStyle := m.th.Styles.Muted
	if n := float64(len(m.snap.CPU.Cores)); n > 0 {
		loadStyle = m.th.Value(n*3/4, n, h.Load[0])
	}
	lines := []string{
		m.kv("NAME", h.Hostname),
		m.kv("SYS", strings.TrimSpace(h.Platform+" "+h.Arch)),
		m.kv("KERNEL", trunc(h.Kernel, 18)),
		m.kv("UPTIME", format.Uptime(h.Uptime)),
		m.kv("LOAD", loadStyle.Render(fmt.Sprintf("%.2f %.2f %.2f", h.Load[0], h.Load[1], h.Load[2]))),
		m.kv("PROCS", strconv.Itoa(h.Procs)),
	}
	if s := m.hotSensor(); s != nil {
		style := m.th.Value(m.cfg.Thresholds.TempWarn, m.cfg.Thresholds.TempCrit, s.TempC)
		lines = append(lines, m.kv("TEMP", style.Render(fmt.Sprintf("%.0f°C", s.TempC))+
			m.th.Styles.Muted.Render(" "+trunc(s.Name, 12))))
	}
	if b := m.snap.Battery; b != nil {
		state := "on battery"
		if b.Charging {
			state = "charging"
		}
		lines = append(lines, m.kv("BATTERY", fmt.Sprintf("%.0f%% (%s)", b.Percent, state)))
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
	return lines
}

// cpuView renders the total gauge, the scrolling graph and per-core bars.
// maxLines bounds the output; per-core rows are trimmed first.
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
	lines = append(lines, strings.Split(m.cpuGraph.View(), "\n")...)

	graphH := len(lines) // head + graph rows; per-core rows fill the rest
	if coreLines := m.perCoreView(innerW, maxLines-graphH); coreLines != "" {
		lines = append(lines, strings.Split(coreLines, "\n")...)
	}
	return lines
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
	bar := canvas.Bar(10, pct/100,
		m.th.Value(m.cfg.Thresholds.CPUWarn, m.cfg.Thresholds.CPUCrit, pct),
		m.th.Styles.Muted,
	)
	val := m.th.Styles.Muted.Render(fmt.Sprintf("%3.0f%%", pct))
	return label + bar + val
}

// memView renders RAM and swap bars in one panel; the history graphs
// trail so they are the first thing dropped on short screens.
func (m *Model) memView(innerW int) []string {
	mem := m.snap.Mem
	barW := clampInt(innerW-14, 12, 28)

	lines := []string{
		m.valuePct(mem.Percent, m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit) +
			m.th.Styles.Muted.Render(fmt.Sprintf("  %s / %s", format.Bytes(mem.Used), format.Bytes(mem.Total))),
		m.bar(barW, mem.Percent, m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit),
	}

	if mem.SwapTotal > 0 {
		lines = append(lines,
			m.valuePct(mem.SwapPercent, m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit)+
				m.th.Styles.Muted.Render(fmt.Sprintf("  SWAP  %s / %s",
					format.Bytes(mem.SwapUsed), format.Bytes(mem.SwapTotal))),
			m.bar(barW, mem.SwapPercent, m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit),
		)
	} else {
		lines = append(lines, m.th.Styles.Muted.Render("SWAP —"))
	}

	lines = append(lines, strings.Split(m.memGraph.View(), "\n")...)
	if mem.SwapTotal > 0 {
		lines = append(lines, strings.Split(m.swapGraph.View(), "\n")...)
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
		lines = append(lines, m.th.Styles.Muted.Render(fmt.Sprintf("%-8s", trunc(n.Name, 8)))+
			fmt.Sprintf("↓%s ↑%s", shortRate(n.RxRate), shortRate(n.TxRate)))
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

	barW := clampInt(innerW-24, 8, 20)
	for _, d := range disks[:min(len(disks), max(0, maxRows))] {
		mount := trunc(filepath.Base(d.Mountpoint), 9)
		name := m.th.Styles.Muted.Render(fmt.Sprintf("%-9s", mount))
		bar := m.bar(barW, d.Percent, m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit)
		pct := m.valuePct(d.Percent, m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit)
		lines = append(lines, name+bar+pct)
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
		cpuStyle := m.th.Value(m.cfg.Thresholds.CPUWarn, m.cfg.Thresholds.CPUCrit, min(p.CPU, 100))
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

// alerts collects threshold crossings, glances-style. Critical findings
// sort before warnings so the strip leads with the worst.
func (m *Model) alerts() []alert {
	if !m.live {
		return nil
	}
	t := m.cfg.Thresholds
	var out []alert

	pct := func(warn, crit, v float64, label string) {
		switch {
		case v >= crit:
			out = append(out, alert{crit: true, text: fmt.Sprintf("%s %.0f%%", label, v)})
		case v >= warn:
			out = append(out, alert{crit: false, text: fmt.Sprintf("%s %.0f%%", label, v)})
		}
	}
	pct(t.CPUWarn, t.CPUCrit, m.snap.CPU.Percent, "CPU")
	pct(t.MemWarn, t.MemCrit, m.snap.Mem.Percent, "MEM")
	pct(t.MemWarn, t.MemCrit, m.snap.Mem.SwapPercent, "SWAP")

	if s := m.hotSensor(); s != nil {
		switch {
		case s.TempC >= t.TempCrit:
			out = append(out, alert{crit: true, text: fmt.Sprintf("TEMP %.0f°C", s.TempC)})
		case s.TempC >= t.TempWarn:
			out = append(out, alert{crit: false, text: fmt.Sprintf("TEMP %.0f°C", s.TempC)})
		}
	}

	var worst *collector.Disk
	for i := range m.snap.Disks {
		if worst == nil || m.snap.Disks[i].Percent > worst.Percent {
			worst = &m.snap.Disks[i]
		}
	}
	if worst != nil {
		pct(t.MemWarn, t.MemCrit, worst.Percent, "DISK "+filepath.Base(worst.Mountpoint))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].crit && !out[j].crit })
	return out
}

// alert is one threshold crossing shown in the strip.
type alert struct {
	crit bool
	text string
}

func (m *Model) alertsView() string {
	al := m.alerts()
	if len(al) == 0 {
		return ""
	}
	texts := make([]string, len(al))
	crit := false
	for i, a := range al {
		texts[i] = a.text
		crit = crit || a.crit
	}
	style := m.th.Styles.Warn
	if crit {
		style = m.th.Styles.Crit
	}
	return style.Render("⚠ " + strings.Join(texts, " · "))
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

// hotSensor returns the hottest reported sensor, if any.
func (m *Model) hotSensor() *collector.Sensor {
	if len(m.snap.Sensors) == 0 {
		return nil
	}
	return &m.snap.Sensors[0]
}

// valuePct renders a percentage with threshold coloring.
func (m *Model) valuePct(pct, warn, crit float64) string {
	return m.th.Value(warn, crit, pct).Render(fmt.Sprintf("%.1f%%", pct))
}

// bar renders a threshold-colored usage bar.
func (m *Model) bar(w int, pct, warn, crit float64) string {
	return canvas.Bar(w, pct/100, m.th.Value(warn, crit, pct), m.th.Styles.Muted)
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
