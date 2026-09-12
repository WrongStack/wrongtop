package dashboard

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/format"
	"github.com/wrongstack/wrongtop/internal/ui"
	"github.com/wrongstack/wrongtop/internal/ui/canvas"
)

// hostView renders the identity panel: one kv row per fact, plus
// temperature, extra sensors, battery and fan rows when the platform
// reports them, and a load-average graph when the row budget allows.
// The graph trails so tight budgets drop it first.
func (m *Model) hostView(innerW, maxRows int) []string {
	h := m.snap.Host
	valW := max(10, innerW-9) // kv labels pad to 8 cells + one space
	loadStyle := m.th.Styles.Muted
	cores := float64(len(m.snap.CPU.Cores))
	loadMeter := ""
	if cores > 0 {
		loadStyle = m.th.Value(cores*3/4, cores, h.Load[0])
		// a small meter scaled to the core count, btop-style
		loadMeter = " " + canvas.GradientBar(8, clamp01(h.Load[0]/cores), m.th.Ramps.CPU, m.th.Styles.Track)
	}
	lines := []string{
		m.kv("NAME", ui.Trunc(h.Hostname, valW)),
		m.kv("SYS", ui.Trunc(strings.TrimSpace(h.Platform+" "+h.Arch), valW)),
	}
	if h.OS != "" { // the full OS name is only shown when the platform reports it
		lines = append(lines, m.kv("OS", ui.Trunc(h.OS, valW)))
	}
	lines = append(lines,
		m.kv("KERNEL", ui.Trunc(h.Kernel, valW)),
		m.kv("LOAD", loadStyle.Render(fmt.Sprintf("%.2f %.2f %.2f", h.Load[0], h.Load[1], h.Load[2]))+loadMeter),
		m.kv("PROCS", strconv.Itoa(h.Procs)),
	)
	if h.Users > 0 {
		lines = append(lines, m.kv("USERS", strconv.Itoa(h.Users)))
	}
	if s := m.hotSensor(); s != nil {
		style := m.th.Value(m.cfg.Thresholds.TempWarn, m.cfg.Thresholds.TempCrit, s.TempC)
		lines = append(lines, m.kv("TEMP", style.Render(fmt.Sprintf("%.0f°C", s.TempC))+
			m.th.Styles.Muted.Render(" "+ui.Trunc(s.Name, max(8, valW-6)))))
	}
	// secondary sensors, hottest first already shown above
	if rest := m.restSensors(); len(rest) > 0 {
		if line := m.sensorList(rest, valW); line != "" {
			lines = append(lines, m.kv("SENS", line))
		}
	}
	if b := m.snap.Battery; b != nil {
		state := "on battery"
		if b.Charging {
			state = "charging " + ui.Icon("bat", m.cfg.NerdFonts)
		}
		meter := canvas.GradientBar(8, b.Percent/100, m.th.Ramps.Bat, m.th.Styles.Track)
		lines = append(lines, m.kv("BATTERY",
			fmt.Sprintf("%.0f%% ", b.Percent)+meter+m.th.Styles.Muted.Render(" "+state)))
	}
	if len(m.snap.Fans) > 0 {
		lines = append(lines, m.kv("FANS", m.fansRow(valW)))
	}
	// the load graph trails: tight budgets truncate it first
	if maxRows-len(lines) >= 2 {
		lines = append(lines, strings.Split(m.loadGraph.View(), "\n")...)
	}
	return lines
}

// fansRow renders the fan readings with their names when the platform
// reports them ("CPU 1200rpm · GPU 940rpm"), falling back to bare RPMs
// when the named row would not fit the panel.
func (m *Model) fansRow(maxW int) string {
	fans := m.snap.Fans[:min(len(m.snap.Fans), 3)]
	named := make([]string, len(fans))
	bare := make([]string, len(fans))
	for i, f := range fans {
		name := ui.Trunc(f.Name, 6)
		if name == "" {
			name = fmt.Sprintf("F%d", i)
		}
		named[i] = m.th.Styles.Muted.Render(name+" ") + fmt.Sprintf("%.0frpm", f.RPM)
		bare[i] = fmt.Sprintf("%.0frpm", f.RPM)
	}
	row := strings.Join(named, m.th.Styles.Muted.Render(" · "))
	if lipgloss.Width(row) > maxW {
		row = strings.Join(bare, " · ")
	}
	if extra := len(m.snap.Fans) - len(fans); extra > 0 {
		row += m.th.Styles.Muted.Render(fmt.Sprintf("  +%d", extra))
	}
	return row
}

// cpuView renders the CPU panel: a full-width gradient meter, then the
// total graph, then — on wide grid panels — a btop-style grid of
// per-core rolling graphs. Narrow panels keep the compact context line
// and one-row per-core bars instead.
func (m *Model) cpuView(innerW, maxLines int) []string {
	c := m.snap.CPU
	wide := m.heroWorth(innerW, maxLines)
	var lines []string

	if !wide {
		// compact context line: clock and busiest single core (the value
		// and temperature live on the border line)
		var head string
		// gopsutil reports garbage sub-100MHz clocks on Apple Silicon; a real
		// core never idles below ~800MHz, so hide anything under 200.
		if c.FreqMHz >= 200 { //nolint:mnd // plausibility floor
			head = fmt.Sprintf("%.1fGHz", c.FreqMHz/1000)
		}
		if len(c.Cores) > 0 {
			mx := c.Cores[0]
			for _, v := range c.Cores[1:] {
				mx = max(mx, v)
			}
			if head != "" {
				head += "  ·  "
			}
			head += fmt.Sprintf("max %.0f%%", mx)
		}
		if head != "" {
			lines = append(lines, m.th.Styles.Muted.Render(head))
		}
		if sl := m.sensorsLine(innerW); sl != "" {
			lines = append(lines, sl)
		}
	}

	if innerW > 20 { // full-width gradient meter, btop-style
		lines = append(lines, canvas.GradientBar(innerW, c.Percent/100, m.th.Ramps.CPU, m.th.Styles.Track))
	}

	lines = append(lines, strings.Split(m.cpuGraph.View(), "\n")...)
	graphH := len(lines)

	if wide {
		if grid := m.coreGraphGrid(innerW, maxLines-graphH); grid != "" {
			lines = append(lines, strings.Split(grid, "\n")...)
		}
	} else if m.density < densityCompact {
		if coreLines := m.perCoreView(innerW, maxLines-graphH); coreLines != "" {
			lines = append(lines, strings.Split(coreLines, "\n")...)
		}
	}
	return lines
}

// heroWorth reports whether the CPU panel gets the wide treatment:
// full-width total graph plus the per-core graph grid. This must stay in
// sync with SetSize, which sizes the total graph to the full panel under
// the same condition.
func (m *Model) heroWorth(innerW, maxLines int) bool {
	return m.density == densityFull && m.class() == layoutGrid && innerW >= 56
}

// coreGraphGrid renders the per-core rolling graphs as rows of cells —
// the btop look that keeps the wide CPU panel alive even at idle: label
// and value ride above each core's own two-row history. maxRows bounds
// the output in whole cell rows.
func (m *Model) coreGraphGrid(innerW, maxRows int) string {
	cores := m.snap.CPU.Cores
	if len(cores) == 0 || maxRows < 3 {
		return ""
	}
	graphW := m.coreGraphW(innerW)
	cellsPerRow := clampInt(innerW/(graphW+7), 1, 8)
	cellRows := maxRows / 3

	shown := min(len(cores), cellsPerRow*cellRows)
	out := make([]string, 0, cellRows*3)
	for r := 0; r*cellsPerRow < shown; r++ {
		cells := make([]string, 0, cellsPerRow*2)
		for c := 0; c < cellsPerRow; c++ {
			i := r*cellsPerRow + c
			if i >= shown {
				break
			}
			if c > 0 {
				cells = append(cells, "  ")
			}
			// label tinted by the core's own heat: a quick glance shows
			// which cores are busy even when their graphs are flat idle
			label := m.th.Ramps.CPU.StyleAt(clamp01(cores[i] / 100)).Render(fmt.Sprintf("c%-2d", i))
			val := m.th.Styles.Muted.Render(fmt.Sprintf("%3.0f%%", cores[i]))
			block := label + " " + val + "\n" + m.coreGraphs[i].View()
			cells = append(cells, block)
		}
		out = append(out, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}
	if hidden := len(cores) - shown; hidden > 0 && len(out) < maxRows {
		out = append(out, m.th.Styles.Muted.Render(fmt.Sprintf("… +%d more cores", hidden)))
	}
	return strings.Join(out, "\n")
}

// heroCard lifts the big digits onto a softly tinted plate with one
// column of air from the panel border — the digits only appear in the
// narrow MEMORY panel, where they own half the width.
func (m *Model) heroCard(digits []string) []string {
	plate := lipgloss.NewStyle().Background(
		lipgloss.Color(canvas.Ramp{m.th.Palette.BG, m.th.Palette.FG}.At(0.07)))
	out := make([]string, len(digits))
	for i, l := range digits {
		out[i] = plate.Render(" ") + plate.Render(l)
	}
	return out
}

// perCoreView renders mini bars, up to perRow per line, bounded by
// maxRows. Bars adapt to the panel: wide panels get longer meters and
// two-space gutters instead of the cramped single-column gap.
func (m *Model) perCoreView(innerW, maxRows int) string {
	if len(m.snap.CPU.Cores) == 0 || maxRows < 1 {
		return ""
	}
	perRow := clampInt(innerW/21, 2, 6)
	barW := clampInt((innerW-2*(perRow-1))/perRow-7, 10, 16)

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
		cells := make([]string, 0, perRow*2-1)
		for c := 0; c < perRow; c++ {
			i := r*perRow + c
			if i >= shown {
				break
			}
			if c > 0 {
				cells = append(cells, "  ") // keep neighbors from running together
			}
			cells = append(cells, m.coreBar(i, m.snap.CPU.Cores[i], barW))
		}
		out = append(out, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}
	if hidden > 0 {
		out = append(out, m.th.Styles.Muted.Render(fmt.Sprintf("… +%d more cores", hidden)))
	}
	return strings.Join(out, "\n")
}

func (m *Model) coreBar(i int, pct float64, barW int) string {
	// heat-tinted label, matching the wide panel's per-core grid
	label := m.th.Ramps.CPU.StyleAt(clamp01(pct / 100)).Render(fmt.Sprintf("c%-2d", i))
	bar := canvas.GradientBar(barW, pct/100, m.th.Ramps.CPU, m.th.Styles.Track)
	val := m.th.Styles.Muted.Render(fmt.Sprintf("%4.0f%%", pct))
	return label + bar + val
}

// memView renders gradient meters for RAM, swap and zram — with a
// big-digit hero card on wide panels; the history graphs trail so they
// are the first thing dropped on short screens. Meters span the panel:
// the old fixed 28-cell cap left them short while CPU ran full width.
func (m *Model) memView(innerW int) []string {
	mem := m.snap.Mem

	heroMode := innerW >= 30 && m.density != densityMinimal
	heroW := 0
	var big []string
	if heroMode {
		big = m.heroCard(canvas.BigNumber(int(mem.Percent+0.5), m.th.Ramps.Mem))
		heroW = lipgloss.Width(big[0]) + 2
	}
	rightW := max(10, innerW-heroW)
	barW := clampInt(rightW-14, 10, 64)

	// one label helper keeps RAM/SWAP/ZRAM rows column-aligned
	key := func(k, body string) string {
		return m.th.Styles.Muted.Render(fmt.Sprintf("%-4s ", k)) + body
	}
	// pair values degrade in tiers so narrow heroes never clip mid-unit;
	// budget is the cells available after the row's fixed prefixes
	pair := func(used, total uint64, budget int) string {
		s := fmt.Sprintf("%s / %s", format.Bytes(used), format.Bytes(total))
		if lipgloss.Width(s) > budget {
			s = fmt.Sprintf("%s/%s", format.BytesCompact(used), format.BytesCompact(total))
			s = strings.ReplaceAll(s, " ", "")
		}
		return s
	}

	ramVals := pair(mem.Used, mem.Total, rightW-5) // "RAM  " prefix
	ramLine := key("RAM", ramVals)
	if avail := format.BytesCompact(mem.Available); mem.Available > 0 &&
		lipgloss.Width(ramLine)+4+lipgloss.Width(avail) <= rightW {
		ramLine += m.th.Styles.Muted.Render(" · " + avail + " avail")
	}

	right := []string{
		ramLine,
		canvas.GradientBar(barW, mem.Percent/100, m.th.Ramps.Mem, m.th.Styles.Track),
	}

	if mem.SwapTotal > 0 {
		right = append(right,
			m.valuePct(mem.SwapPercent, m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit)+
				m.th.Styles.Muted.Render(" ")+key("SWAP", pair(mem.SwapUsed, mem.SwapTotal, rightW-11)),
			canvas.GradientBar(barW, mem.SwapPercent/100, m.th.Ramps.Swap, m.th.Styles.Track),
		)
	} else {
		right = append(right, m.th.Styles.Muted.Render("SWAP —"))
	}

	if mem.ZramTotal > 0 { // linux compressed swap device
		pct := float64(mem.ZramUsed) / float64(mem.ZramTotal) * 100
		right = append(right,
			m.valuePct(pct, m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit)+
				m.th.Styles.Muted.Render(" ")+key("ZRAM", pair(mem.ZramUsed, mem.ZramTotal, rightW-11)),
			canvas.GradientBar(barW, pct/100, m.th.Ramps.IO, m.th.Styles.Track),
		)
	}

	var lines []string
	if heroMode {
		// the hero column stays padded on every row so the meters never
		// slide left once the digits run out
		blank := strings.Repeat(" ", heroW)
		for i := 0; i < max(len(big), len(right)); i++ {
			l, r := blank, ""
			if i < len(big) {
				l = big[i] + strings.Repeat(" ", heroW-lipgloss.Width(big[i]))
			}
			if i < len(right) {
				r = right[i]
			}
			lines = append(lines, l+r)
		}
	} else {
		// pct + two spaces + label precede the values here
		lines = append(lines,
			m.valuePct(mem.Percent, m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit)+
				m.th.Styles.Muted.Render("  ")+key("RAM", pair(mem.Used, mem.Total, rightW-12)))
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

// netView renders the total down/up rates, the graphs with their
// auto-scale ceilings labeled in the top-right corners, plus the busiest
// interfaces as glances-style mixed meters: one bar, green share =
// download, blue share = upload, with the total rate alongside. The
// rates lead in-panel: the narrow NETWORK border has no room for a
// readout of its own.
func (m *Model) netView(maxIface int) []string {
	rx, tx := totalRates(m.snap.Nets)
	head := m.th.Styles.OK.Render("↓ "+shortRate(rx)) +
		m.th.Styles.Muted.Render("  ") +
		m.th.Styles.Title.Render("↑ "+shortRate(tx))

	peak := func(label string, v float64) string {
		if v <= 0 {
			return ""
		}
		return m.th.Styles.Muted.Render(label + " " + shortRate(v))
	}
	lines := []string{head}
	lines = append(lines, strings.Split(
		canvas.OverlayLabel(m.rxGraph.View(), peak("↓pk", m.rxScale.Max())), "\n")...)
	lines = append(lines, strings.Split(
		canvas.OverlayLabel(m.txGraph.View(), peak("↑pk", m.txScale.Max())), "\n")...)

	ifaces := make([]collector.NetIface, len(m.snap.Nets))
	copy(ifaces, m.snap.Nets)
	sort.Slice(ifaces, func(i, j int) bool {
		return ifaces[i].RxRate+ifaces[i].TxRate > ifaces[j].RxRate+ifaces[j].TxRate
	})
	for _, n := range ifaces[:min(len(ifaces), max(0, maxIface))] {
		total := n.RxRate + n.TxRate
		share := 0.5
		if total > 0 {
			share = n.RxRate / total
		}
		style := m.th.Styles.OK // green for download-dominated
		if n.TxRate > n.RxRate {
			style = m.th.Styles.Title // upload-dominated
		}
		lines = append(lines, m.th.Styles.Muted.Render(fmt.Sprintf("%-10s", ui.Trunc(n.Name, 10)))+
			canvas.DualBar(10, share, m.th.Ramps.RX, m.th.Ramps.TX)+" "+style.Render(shortRate(total)))
	}
	return lines
}

// diskView renders the busiest device's busy meter when the panel is
// wide, plus usage bars per mount with their I/O sparklines. The R/W
// totals live on the border readout.
func (m *Model) diskView(innerW, maxRows int) []string {
	var lines []string
	if innerW >= 46 {
		busyName, busyPct := "", 0.0
		for _, io := range m.snap.DiskIOs {
			if io.BusyPercent > busyPct {
				busyPct, busyName = io.BusyPercent, io.Name
			}
		}
		if busyName != "" {
			busyStyle := m.th.Value(60, 90, busyPct) //nolint:mnd // busy thresholds
			lines = append(lines,
				m.th.Styles.Muted.Render(ui.Trunc(busyName, 10)+" ")+
					busyStyle.Render(canvas.GradientBar(
						clampInt(innerW-16, 6, 20), busyPct/100, m.th.Ramps.IO, m.th.Styles.Track))+
					busyStyle.Render(fmt.Sprintf(" %.0f%%", busyPct)))
		}
	}

	disks := make([]collector.Disk, len(m.snap.Disks))
	copy(disks, m.snap.Disks)
	sort.Slice(disks, func(i, j int) bool { return disks[i].Percent > disks[j].Percent })

	barW := clampInt(innerW-34, 6, 24)
	rows := max(0, maxRows-len(lines))
	shown := min(len(disks), rows)
	for _, d := range disks[:shown] {
		mount := mountLabel(d.Mountpoint)
		name := m.th.Styles.Muted.Render(fmt.Sprintf("%-10s", ui.Trunc(mount, 10)))
		bar := canvas.GradientBar(barW, d.Percent/100, m.th.Ramps.Mem, m.th.Styles.Track)
		spark := canvas.SparklineScaled(m.sparkFor(d.Device), m.th.Ramps.IO)
		pct := m.valuePct(d.Percent, m.cfg.Thresholds.MemWarn, m.cfg.Thresholds.MemCrit)
		lines = append(lines, name+bar+pct+spark)
	}
	if len(disks) > rows {
		lines = append(lines, m.th.Styles.Muted.Render(fmt.Sprintf("… +%d more", len(disks)-rows)))
	}
	return lines
}

// mountLabel shortens a mountpoint to its label: the path basename,
// which already covers macOS "/Volumes/..." volumes.
func mountLabel(mountpoint string) string {
	if base := filepath.Base(mountpoint); base != "" && base != "/" {
		return base
	}
	return mountpoint
}

// sparkFor returns the I/O history of a mount's device. IO counters are
// keyed by the kernel device name ("disk3", "sda") while mount devices
// carry partition/slice suffixes ("/dev/disk3s5", "/dev/sda1"), so the
// longest IO name that prefixes the device base wins — an exact match on
// platforms whose IO table lists partitions (linux), the whole disk on
// darwin.
func (m *Model) sparkFor(device string) []float64 {
	base := filepath.Base(device)
	best := ""
	var hist []float64
	for name, h := range m.ioHist {
		if len(name) > len(best) && strings.HasPrefix(base, name) {
			best, hist = name, h
		}
	}
	return hist
}

// procView renders the busiest processes, CPU first, under a column
// header. maxRows bounds the total lines; the count lives on the
// border readout.
func (m *Model) procView(innerW, maxRows int) []string {
	procs := make([]collector.Proc, len(m.snap.Procs))
	copy(procs, m.snap.Procs)
	sort.Slice(procs, func(i, j int) bool {
		if procs[i].CPU != procs[j].CPU {
			return procs[i].CPU > procs[j].CPU
		}
		return procs[i].Mem > procs[j].Mem // tiebreak: memory
	})

	var lines []string
	if maxRows > 0 && len(procs) > 0 {
		lines = append(lines, m.th.Styles.Muted.Render(
			fmt.Sprintf("%7s %-9s %5s %5s %8s  %s", "", "USER", "MEM%", "CPU%", "RSS", "NAME")))
	}

	nameW := max(10, innerW-38)
	for _, p := range procs[:min(len(procs), max(0, maxRows-1))] {
		// CPU colored along the value ramp instead of three flat states
		cpuStyle := m.th.Ramps.CPU.StyleAt(min(p.CPU, 100) / 100)
		lines = append(lines,
			m.th.Styles.Muted.Render(fmt.Sprintf("%7d ", p.PID))+
				fmt.Sprintf("%-9s", ui.Trunc(p.User, 9))+
				m.th.Styles.Muted.Render(fmt.Sprintf("%5.1f ", p.Mem))+
				cpuStyle.Render(format.CPUPct(p.CPU))+" "+
				fmt.Sprintf("%8s ", format.Bytes(p.RSS))+
				ui.Trunc(p.Name, nameW),
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
		label := ui.Trunc(mountLabel(worst.Mountpoint), 12) // APFS volume names run long
		pct("disk:"+worst.Mountpoint, "DISK "+label, t.MemWarn, t.MemCrit, worst.Percent)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Crit && !out[j].Crit })
	return out
}

// sensorsLine renders every reported temperature on one line, hottest
// first, each value threshold-colored. The hottest reading already shows
// on the border readout, so the line only appears when a second sensor
// exists. Width-bounded to the panel.
func (m *Model) sensorsLine(maxW int) string {
	if len(m.snap.Sensors) < 2 {
		return ""
	}
	line := m.th.Styles.Muted.Render("SENS ")
	for i, s := range m.snap.Sensors {
		style := m.th.Value(m.cfg.Thresholds.TempWarn, m.cfg.Thresholds.TempCrit, s.TempC)
		cell := style.Render(fmt.Sprintf("%.0f°", s.TempC)) +
			m.th.Styles.Muted.Render(" "+ui.Trunc(s.Name, 10))
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

// sensorList renders the given sensors as "61° name" cells joined by
// separators, bounded to maxW; returns "" when nothing fits.
func (m *Model) sensorList(sensors []collector.Sensor, maxW int) string {
	var line string
	sep := ""
	for _, s := range sensors {
		style := m.th.Value(m.cfg.Thresholds.TempWarn, m.cfg.Thresholds.TempCrit, s.TempC)
		cell := style.Render(fmt.Sprintf("%.0f°", s.TempC)) +
			m.th.Styles.Muted.Render(" "+ui.Trunc(s.Name, 10))
		if lipgloss.Width(line)+lipgloss.Width(sep)+lipgloss.Width(cell) > maxW {
			break
		}
		line += sep + cell
		sep = m.th.Styles.Muted.Render(" · ")
	}
	return line
}

// restSensors returns every sensor except the hottest (the list is
// sorted hottest-first by the collector).
func (m *Model) restSensors() []collector.Sensor {
	if len(m.snap.Sensors) < 2 {
		return nil
	}
	return m.snap.Sensors[1:]
}

// gpuLines renders one line per adapter: name, utilization bar, VRAM
// (with a meter on wide panels) and temperature. The adapter count and
// name live on the border readout.
func (m *Model) gpuLines(innerW, maxLines int) []string {
	gpus := m.snap.GPUs
	if len(gpus) == 0 || maxLines < 1 {
		return nil
	}
	var lines []string
	shown := min(len(gpus), maxLines)
	nameW := clampInt(innerW-34, 12, 44)
	for _, g := range gpus[:shown] {
		barW := clampInt(innerW-42, 8, 22)
		bar := canvas.GradientBar(barW, clamp01(g.Util/100), m.th.Ramps.CPU, m.th.Styles.Track)
		mem := ""
		if g.MemTotal > 0 {
			if innerW >= 64 {
				vram := clamp01(float64(g.MemUsed) / float64(g.MemTotal))
				mem = " " + canvas.GradientBar(8, vram, m.th.Ramps.Mem, m.th.Styles.Track)
			}
			mem += fmt.Sprintf(" %s/%s", format.BytesCompact(g.MemUsed), format.BytesCompact(g.MemTotal))
		}
		temp := m.th.Value(m.cfg.Thresholds.TempWarn, m.cfg.Thresholds.TempCrit, g.TempC).
			Render(fmt.Sprintf("%.0f°", g.TempC))
		lines = append(lines,
			m.th.Styles.Muted.Render(fmt.Sprintf("GPU%d ", g.Index))+
				ui.Trunc(g.Name, nameW)+" "+bar+fmt.Sprintf("%4.0f%%", g.Util)+mem+" "+temp)
	}
	return lines
}

// Border readouts: title left, live value right — the btop panel
// signature. Values are pre-styled strings spliced into the border.

func (m *Model) hostRight() string {
	return m.th.Styles.Muted.Render("up ") + m.th.Styles.FG.Render(format.Uptime(m.snap.Host.Uptime))
}

func (m *Model) cpuRight() string {
	c := m.snap.CPU
	parts := []string{m.valuePct(c.Percent, m.cfg.Thresholds.CPUWarn, m.cfg.Thresholds.CPUCrit)}
	if s := m.hotSensor(); s != nil {
		st := m.th.Value(m.cfg.Thresholds.TempWarn, m.cfg.Thresholds.TempCrit, s.TempC)
		parts = append(parts, st.Render(fmt.Sprintf("%.0f°C", s.TempC)))
	}
	return strings.Join(parts, m.th.Styles.Muted.Render(" · "))
}

func (m *Model) memRight() string {
	mem := m.snap.Mem
	// the hero digits carry the percentage, so the border shows the
	// used/total pair in its compact form
	pair := fmt.Sprintf("%s/%s", format.BytesCompact(mem.Used), format.BytesCompact(mem.Total))
	return m.th.Styles.FG.Render(pair)
}

func (m *Model) diskRight() string {
	var rb, wb float64
	for _, io := range m.snap.DiskIOs {
		rb += io.ReadBytes
		wb += io.WriteBytes
	}
	return m.th.Styles.Muted.Render("R ") + m.th.Styles.FG.Render(format.RateFixed(10, rb)) +
		m.th.Styles.Muted.Render(" · W ") + m.th.Styles.FG.Render(format.RateFixed(10, wb))
}

func (m *Model) gpuRight() string {
	gpus := m.snap.GPUs
	switch len(gpus) {
	case 0:
		return ""
	case 1:
		return m.th.Styles.FG.Render(ui.Trunc(gpus[0].Name, 28))
	default:
		return m.th.Styles.Muted.Render(fmt.Sprintf("%d adapters", len(gpus)))
	}
}

func (m *Model) procRight() string {
	return m.th.Styles.FG.Render(fmt.Sprintf("%d procs", len(m.snap.Procs)))
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
