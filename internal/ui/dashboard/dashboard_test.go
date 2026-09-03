package dashboard

import (
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
		CPU: collector.CPU{Percent: 45.2, Cores: []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 5}},
		Mem: collector.Mem{
			Total: 16 << 30, Used: 10 << 30, Available: 6 << 30, Percent: 62.5,
			SwapTotal: 2 << 30, SwapUsed: 1 << 29, SwapPercent: 25,
		},
	}
}

func TestViewRendersSnapshot(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(120, 38)
	m.Update(collector.SnapshotMsg{Snap: fakeSnapshot()})
	m.Update(collector.SnapshotMsg{Snap: fakeSnapshot()}) // graphs need 2+ samples
	out := m.View()

	for _, want := range []string{"testhost", "macOS 15.6 arm64", "3d 0h", "TOTAL", "45.2%", "62.5%", "HOST", "CPU", "MEM", "SWAP"} {
		if !contains(out, want) {
			t.Errorf("view missing %q", want)
		}
	}
	t.Log("\n" + out)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
