package app

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/ui"
)

// zzHostile builds a wire-legal extreme snapshot: every numeric at ±1e308
// or max-int extremes, every string empty or oversized (CJK included),
// every optional section populated, 700 processes. JSON cannot carry
// NaN/Inf, so nothing here violates the wire contract.
func zzHostile() collector.Snapshot {
	procs := make([]collector.Proc, 0, 700)
	for i := 0; i < 700; i++ {
		procs = append(procs, collector.Proc{
			PID:     int32((i * 7919) % 2147483647),
			PPID:    1,
			Name:    strings.Repeat("名", 100) + strconv.Itoa(i),
			CPU:     1e18,
			Mem:     -1e18,
			RSS:     ^uint64(0),
			Threads: math.MaxInt32,
			Nice:    math.MinInt8,
			User:    strings.Repeat("ü", 100),
			State:   "Z",
		})
	}
	return collector.Snapshot{
		Time: time.Now(),
		Host: collector.Host{
			Hostname: strings.Repeat("h", 10000),
			OS:       strings.Repeat("o", 500),
			Platform: strings.Repeat("p", 500),
			Kernel:   strings.Repeat("k", 500),
			Arch:     strings.Repeat("a", 500),
			Uptime:   time.Duration(1) << 62,
			Procs:    math.MaxInt32,
			Load:     [3]float64{1e308, -1e308, 0},
			Users:    math.MaxInt32,
		},
		CPU: collector.CPU{
			Percent: 1e308,
			FreqMHz: -1e308,
			Cores:   []float64{0, -1e308, 1e308, 100, -100, 50.5, 0.001, math.MaxFloat64},
		},
		Mem: collector.Mem{
			Total: ^uint64(0), Used: ^uint64(0), Available: 0,
			Percent:   1e308,
			SwapTotal: ^uint64(0), SwapUsed: 123, SwapPercent: -1e308,
			ZramTotal: 100, ZramUsed: 50,
		},
		Sensors: []collector.Sensor{
			{Name: strings.Repeat("s", 1000), TempC: 1e308},
			{Name: "", TempC: -1e308},
		},
		Fans: []collector.Fan{
			{Name: strings.Repeat("f", 500), RPM: 1e308},
			{Name: "", RPM: -1},
		},
		Battery: &collector.Battery{Percent: -123.456, Charging: true},
		GPUs: []collector.GPU{{
			Index: math.MaxInt32, Name: strings.Repeat("g", 300),
			Util: 1e308, MemUsed: ^uint64(0), MemTotal: 1, TempC: -999,
		}},
		Procs: procs,
		Disks: []collector.Disk{
			{Device: "", Mountpoint: "", FSType: strings.Repeat("t", 1000),
				Total: ^uint64(0), Used: ^uint64(0), Free: 0, Percent: 1e308},
			{Device: "/dev/日本語disk", Mountpoint: "/", FSType: "", Percent: -50},
		},
		DiskIOs: []collector.DiskIO{{
			Name:      strings.Repeat("d", 500),
			ReadBytes: -1e308, WriteBytes: 1e308,
			ReadIOPS: 1e308, WriteIOPS: -1e308, BusyPercent: 1e308,
		}},
		Nets: []collector.NetIface{
			{Name: strings.Repeat("n", 400), RxRate: 1e308, TxRate: -1e308,
				RxRatePackets: 1e308, RxDrop: -1e308, TxDrop: 1e308, RxTotal: ^uint64(0)},
			{Name: "", RxRate: 0.5, TxRate: 0.5},
		},
		Conns: []collector.Conn{
			{Local: strings.Repeat("l", 1000), State: strings.Repeat("ESTABLISHED", 1000), PID: math.MaxInt32},
			{Local: "", Remote: "", State: "", PID: math.MinInt32},
		},
	}
}

// TestZZRound51HostileFuzz drives the real app model end to end: every
// size in the matrix (including layout boundaries and degenerate ones),
// four hostile snapshot variants, every tab, and the full key surface
// (density, help, alerts, overlays, tab switching). Containment holds
// when no composed line exceeds the terminal width and the frame fits
// the terminal height (height ≥ 2; height 1 is the documented degenerate
// edge where the two chrome rows cannot both fit).
func TestZZRound51HostileFuzz(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, "", "test")

	extreme := zzHostile()
	empty := collector.Snapshot{}
	zeroed := collector.Snapshot{Time: time.Now()}
	future := zzHostile()
	future.Time = time.Now().Add(1 << 50)

	snaps := []struct {
		name string
		snap collector.Snapshot
	}{
		{"empty", empty}, {"zeroed", zeroed}, {"extreme", extreme}, {"future", future},
	}
	sizes := [][2]int{
		{1, 1}, {20, 6}, {39, 10}, {60, 20}, {79, 23}, {80, 24}, {100, 30},
		{111, 24}, {112, 24}, {146, 40}, {200, 60}, {300, 100},
	}
	keys := []tea.KeyPressMsg{
		key("p"), key("p"), key("p"),
		key("?"), {Code: tea.KeyEscape},
		key("a"), {Code: tea.KeyEscape},
		{Code: tea.KeyTab}, {Code: tea.KeyTab}, {Code: tea.KeyTab},
		{Code: tea.KeyTab}, {Code: tea.KeyTab}, {Code: tea.KeyTab},
		key("1"), key("2"), key("3"), key("4"), key("5"), key("6"), key("7"),
	}

	check := func(frame string, w, h int, phase string) {
		t.Helper()
		lines := strings.Split(frame, "\n")
		if h >= 2 && len(lines) > h {
			t.Fatalf("FAIL [%s]: frame height %d exceeds terminal %d", phase, len(lines), h)
		}
		for i, l := range lines {
			if lw := lipgloss.Width(l); lw > w {
				t.Fatalf("FAIL [%s]: line %d width %d exceeds terminal %d: %q",
					phase, i, lw, w, ui.Trunc(l, 80))
			}
		}
	}

	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		m.Update(tea.WindowSizeMsg{Width: w, Height: h})
		for _, sc := range snaps {
			m.Update(collector.SnapshotMsg{Snap: sc.snap})
			for _, k := range keys {
				m.Update(k)
				check(m.frame(), w, h, sc.name+"/"+strconv.Itoa(w)+"x"+strconv.Itoa(h))
			}
		}
		// direct per-tab drives: every tab fed the hostile snapshots at
		// this size, no panic and a render out of each
		for _, tab := range m.tabs {
			tab.SetSize(w, max(1, h-2))
			for _, sc := range snaps {
				tab.Update(collector.SnapshotMsg{Snap: sc.snap})
				if out := tab.View(); lipgloss.Width(out) < 0 {
					t.Fatalf("FAIL: negative-width render")
				}
			}
		}
	}
}
