package app

import (
	"strings"
	"testing"
)

// TestShortRateFixedContract pins shortRateFixed's documented contract
// (app.go): the "/s" suffix is stripped and the result is padded to a
// stable width so the status-bar chip pair does not jitter sideways as
// magnitudes cross unit boundaries. Sub-unit rates keep format.Rate's
// capital "B"; unit rates use the prefix + lowercase "b" convention.
func TestShortRateFixedContract(t *testing.T) {
	const width = 9

	cases := []struct {
		name       string
		bps        float64
		want       string // trimmed content
		stableSize bool   // realistic magnitudes must render at the fixed width
	}{
		{"zero", 0, "0 B", true},
		{"sub-unit", 986, "986 B", true},
		{"kilo boundary", 1234, "1.2 Kb", true},
		{"two-digit kilo", 12345, "12.3 Kb", true},
		{"three-digit kilo", 123456, "123.5 Kb", true},
		{"mega", 1.2e6, "1.2 Mb", true},
		{"giga", 1.5e9, "1.5 Gb", true},
		{"tera", 2.4e12, "2.4 Tb", true},
		{"exa clamp", 1e21, "1000.0 Eb", true},
		{"beyond last unit", 1e30, "1000000000000.0 Eb", false},
	}

	for _, tc := range cases {
		got := shortRateFixed(tc.bps)
		if strings.Contains(got, "/s") {
			t.Errorf("FAIL: shortRateFixed(%g) = %q: the /s suffix must be stripped", tc.bps, got)
			continue
		}
		if strings.TrimSpace(got) != tc.want {
			t.Errorf("FAIL: shortRateFixed(%g) = %q, want %q padded to %d cells", tc.bps, got, tc.want, width)
		}
		if tc.stableSize && len(got) != width {
			t.Errorf("FAIL: shortRateFixed(%g) = %q: width %d, want stable %d", tc.bps, got, len(got), width)
		}
	}
}
