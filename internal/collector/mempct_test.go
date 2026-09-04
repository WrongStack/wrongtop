package collector

import (
	"context"
	"math"
	"testing"

	"github.com/shirou/gopsutil/v4/mem"
)

// Regression: Proc.Mem used to stay 0 because it was never assigned; the
// MEM% column, the mem sort key and the dashboard process panel all read
// from it. Runs against the live system so it holds on every platform.
func TestProcMemPercentPopulated(t *testing.T) {
	vm, err := mem.VirtualMemory()
	if err != nil || vm.Total == 0 {
		t.Skipf("host memory unavailable: %v", err)
	}

	var c Collector
	procs := c.collectProcs(context.Background(), 1, vm.Total)
	if len(procs) == 0 {
		t.Fatal("no processes collected")
	}

	nonzero := 0
	for _, p := range procs {
		want := float64(p.RSS) / float64(vm.Total) * 100
		if math.Abs(p.Mem-want) > 0.01 {
			t.Errorf("pid %d: Mem = %.3f, want %.3f (rss %d / total %d)",
				p.PID, p.Mem, want, p.RSS, vm.Total)
		}
		if p.RSS > 0 && p.Mem > 0 {
			nonzero++
		}
	}
	if nonzero == 0 {
		t.Fatal("no process with RSS > 0 reported MEM% > 0")
	}
}

func TestMemPercentEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		rss  uint64
		mem  uint64
		want float64
	}{
		{"zero rss", 0, 1000, 0},
		{"zero total", 100, 0, 0},
		{"half", 500, 1000, 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := memPercent(tc.rss, tc.mem); got != tc.want {
				t.Errorf("memPercent(%d, %d) = %v, want %v", tc.rss, tc.mem, got, tc.want)
			}
		})
	}
}
