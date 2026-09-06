package format

import (
	"math"
	"strings"
	"testing"
	"time"
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

// TestRateBeyondLastUnit pins the unit clamp on Rate: remote snapshots
// arrive as JSON from a semi-trusted host, so a rate can be any finite
// float. Before the clamp, any value >= 1e21 indexed "KMGTPE"[6] and
// panicked the render, and +Inf spun the scaling loop forever.
func TestRateBeyondLastUnit(t *testing.T) {
	cases := []struct {
		bps  float64
		want string
	}{
		{1e18, "1.0 Eb/s"},             // last unit that scales naturally
		{999e18, "999.0 Eb/s"},         // largest value below the cap
		{1e21, "1000.0 Eb/s"},          // first value that used to panic
		{1e30, "1000000000000.0 Eb/s"}, // far beyond the table, clamped
	}
	for _, c := range cases {
		if got := Rate(c.bps); got != c.want {
			t.Errorf("Rate(%v) = %q, want %q", c.bps, got, c.want)
		}
	}
	// +Inf must terminate at the clamp, not spin forever.
	if got := Rate(math.Inf(1)); got != "+Inf Eb/s" {
		t.Errorf("Rate(+Inf) = %q, want %q", got, "+Inf Eb/s")
	}
}

func TestBytes(t *testing.T) {
	cases := []struct {
		n    uint64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{100 * 1024, "100.0 KiB"},
		{1 << 20, "1.0 MiB"},
		{1 << 30, "1.0 GiB"},
		{1 << 40, "1.0 TiB"},
		{1 << 50, "1.0 PiB"},
		{1 << 60, "1.0 EiB"},
		{math.MaxUint64, "16.0 EiB"},
	}
	for _, c := range cases {
		if got := Bytes(c.n); got != c.want {
			t.Errorf("Bytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestBytesCompact(t *testing.T) {
	cases := []struct {
		n    uint64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{99 * 1024, "99.0 KiB"},
		{200 * 1024, "200 KiB"}, // >= 100 of the unit: decimal dropped
		{1 << 20, "1.0 MiB"},
		{512 << 20, "512 MiB"},
		{1 << 40, "1.0 TiB"},
		{3 << 50, "3.0 PiB"},
	}
	for _, c := range cases {
		if got := BytesCompact(c.n); got != c.want {
			t.Errorf("BytesCompact(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestCPUPct(t *testing.T) {
	cases := []struct {
		v    float64
		want string
	}{
		{0, "  0.0"},
		{3.5, "  3.5"},
		{99.9, " 99.9"},
		{-4.25, " -4.2"},
		{math.NaN(), "  NaN"},
		{100, "  100"},
		{123.45, "  123"},
		{999.9, " 1000"},
		{1000, " 999+"},
		{9876.5, " 999+"},
	}
	for _, c := range cases {
		if got := CPUPct(c.v); got != c.want {
			t.Errorf("CPUPct(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestUptime(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{45 * time.Second, "45s"},
		{60 * time.Second, "1m"},
		{90 * time.Second, "1m"},
		{59 * time.Minute, "59m"},
		{time.Hour, "1h 0m"},
		{90 * time.Minute, "1h 30m"},
		{23*time.Hour + 59*time.Minute, "23h 59m"},
		{24 * time.Hour, "1d 0h"},
		{25 * time.Hour, "1d 1h"},
		{3*24*time.Hour + 4*time.Hour, "3d 4h"},
		{14 * 24 * time.Hour, "14d 0h"},
	}
	for _, c := range cases {
		if got := Uptime(c.d); got != c.want {
			t.Errorf("Uptime(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
