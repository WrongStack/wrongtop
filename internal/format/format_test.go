package format

import (
	"strings"
	"testing"
)

func TestRateFixedStableWidth(t *testing.T) {
	cases := []struct {
		bps  float64
		want string
	}{
		{0, "0 B/s     "},
		{986, "986 B/s   "},
		{1.2e6, "1.2 Mb/s  "},
		{999.9e9, "999.9 Gb/s"},
	}
	for _, c := range cases {
		if got := RateFixed(10, c.bps); got != c.want {
			t.Errorf("RateFixed(10, %v) = %q, want %q", c.bps, got, c.want)
		}
	}
	// a value wider than the budget is never truncated
	if got := RateFixed(4, 1.2e6); !strings.HasPrefix(got, "1.2 Mb/s") {
		t.Errorf("RateFixed must not clip wide values, got %q", got)
	}
}

func TestRate(t *testing.T) {
	if got := Rate(1.2e6); got != "1.2 Mb/s" {
		t.Errorf("Rate = %q", got)
	}
	if got := Rate(340e3); got != "340.0 Kb/s" {
		t.Errorf("Rate = %q", got)
	}
}
