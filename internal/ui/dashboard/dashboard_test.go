package dashboard

import (
	"strings"
	"testing"
	"time"

	"github.com/ersinkoc/wrongtop/internal/collector"
	"github.com/ersinkoc/wrongtop/internal/config"
	"github.com/ersinkoc/wrongtop/internal/theme"
)

func fakeSnapshot() collector.Snapshot {
	return collector.Snapshot{
		Time: time.Now(),
		Host: collector.Host{
			Hostname: "testhost",
			Platform: "macOS 15.6",
			Arch:     "arm64",
			Kernel:   "24.6.0",
			Uptime:   72 * time.Hour,
			Procs:    612,
			Load:     [3]float64{1.24, 0.98, 0.87},
		},
		CPU: collector.CPU{
			Percent: 45.2,
			FreqMHz: 3200,
			Cores:   []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 5},
		},
		Mem: collector.Mem{
			Total: 16 << 30, Used: 10 << 30, Available: 6 << 30, Percent: 62.5,
			SwapTotal: 2 << 30, SwapUsed: 1 << 29, SwapPercent: 25,
		},
		Sensors: []collector.Sensor{{Name: "CPU", TempC: 55}},
		Nets: []collector.NetIface{
			{Name: "en0", RxRate: 1.5e6, TxRate: 3.4e5, RxTotal: 100, TxTotal: 100},
		},
		Disks: []collector.Disk{
			{Device: "/dev/disk3s1", Mountpoint: "/", FSType: "apfs",
				Total: 100 << 30, Used: 60 << 30, Free: 40 << 30, Percent: 60},
		},
		Procs: []collector.Proc{
			{PID: 1, Name: "low", CPU: 0.5, Mem: 0.1, RSS: 1 << 20, User: "root"},
			{PID: 2, Name: "busy", CPU: 88, Mem: 2.5, RSS: 1 << 26, User: "root"},
		},
	}
}

func newModel(t *testing.T, w, h int) *Model {
	t.Helper()
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(w, h)
	for range 2 { // graphs need 2+ samples
		m.Update(collector.SnapshotMsg{Snap: fakeSnapshot()})
	}
	return m
}

// TestGridShowsAllPanels pins the btop-style single-screen layout: every
// panel is visible at once on a large terminal.
func TestGridShowsAllPanels(t *testing.T) {
	m := newModel(t, 120, 38)
	if m.class() != layoutGrid {
		t.Fatalf("120x38 should use the grid layout, got %v", m.class())
	}
	out := m.View()
	for _, want := range []string{
		"HOST", "CPU", "MEMORY", "NETWORK", "DISKS", "PROCESSES",
		"testhost", "45.2%", "62.5%", "busy", "en0",
	} {
		if !contains(out, want) {
			t.Errorf("grid view missing %q", want)
		}
	}
	t.Log("\n" + out)
}

// TestStackedShowsAllPanels covers the fallback band for mid-size and
// narrow terminals.
func TestStackedShowsAllPanels(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {64, 30}} {
		m := newModel(t, size[0], size[1])
		if m.class() == layoutGrid {
			t.Fatalf("%dx%d should not use the grid layout", size[0], size[1])
		}
		out := m.View()
		for _, want := range []string{"HOST", "CPU", "MEMORY", "NETWORK", "DISKS"} {
			if !contains(out, want) {
				t.Errorf("%dx%d view missing %q", size[0], size[1], want)
			}
		}
	}
}

// TestGridHeightFits verifies the grid never draws past the content area.
func TestGridHeightFits(t *testing.T) {
	for _, h := range []int{24, 28, 30, 40, 50} {
		m := newModel(t, 120, h)
		out := m.View()
		if got := strings.Count(out, "\n") + 1; got > h {
			t.Errorf("height %d: view has %d lines", h, got)
		}
	}
}

// TestAlertStrip pins the glances-style warning line.
func TestAlertStrip(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(120, 38)
	snap := fakeSnapshot()
	snap.CPU.Percent = 95 // crit is 90
	m.Update(collector.SnapshotMsg{Snap: snap})

	if al := m.alerts(); len(al) != 1 || !al[0].Crit || !strings.Contains(al[0].Text, "CPU 95%") {
		t.Fatalf("expected one critical CPU alert, got %+v", m.alerts())
	}
	if out := m.View(); !contains(out, "⚠") || !contains(out, "CPU 95%") {
		t.Error("view missing the alert strip")
	}
}

// TestProcPanelSortedByCPU verifies the busiest process leads the panel.
func TestProcPanelSortedByCPU(t *testing.T) {
	m := newModel(t, 120, 38)
	out := strings.Join(m.procView(114, 5), "\n")
	if strings.Index(out, "busy") > strings.Index(out, "low") {
		t.Errorf("busiest process should be listed first:\n%s", out)
	}
}

// TestGraphPush tracks that the rate scales observe network traffic.
func TestGraphPush(t *testing.T) {
	m := newModel(t, 120, 38)
	if m.rxScale.Max() != 1.5e6 || m.txScale.Max() != 3.4e5 {
		t.Errorf("rate scales should match the sample: rx %v tx %v", m.rxScale.Max(), m.txScale.Max())
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
