package collector

import (
	"context"
	"testing"

	"github.com/shirou/gopsutil/v4/cpu"
)

// Regression: gopsutil reports an Apple M4's 4.5GHz P-cores as "4MHz"
// (kHz read as Hz), which dump and serve then emitted as freq_mhz.
func TestAverageMHzDropsImplausibleClocks(t *testing.T) {
	cases := []struct {
		name string
		mhz  []float64
		want float64
	}{
		{"none", nil, 0},
		{"apple silicon unit bug", []float64{4}, 0},
		{"zero", []float64{0, 0}, 0},
		{"real", []float64{3000, 4000}, 3500},
		{"mixed", []float64{4, 2400}, 2400},
	}
	for _, tc := range cases {
		infos := make([]cpu.InfoStat, 0, len(tc.mhz))
		for _, v := range tc.mhz {
			infos = append(infos, cpu.InfoStat{Mhz: v})
		}
		if got := averageMHz(infos); got != tc.want {
			t.Errorf("%s: averageMHz = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Whatever the host reports, the collected clock is either unreported
// or plausible, on every platform.
func TestCollectFreqIsPlausible(t *testing.T) {
	if got := collectFreq(context.Background()); got != 0 && got < minPlausibleMHz {
		t.Fatalf("collectFreq = %v MHz, implausible", got)
	}
}
