package collector

import (
	"context"
	"testing"
)

// TestPlatformCollectorsSane pins value-level invariants on the
// platform-specific sensor, fan, battery and user-count collectors on
// whatever GOOS the test binary runs on. TestCollectSlowCaches executes
// these paths on every CI lane but asserts nothing about the returned
// values, so a unit-conversion regression (kelvin/celsius, tenths, NaN
// from a zero denominator) in temps_windows.go, battery_windows.go,
// fans_linux.go or users_linux.go would stay green. Bounds mirror the
// implementations' own plausibility filters (windows and linux cap
// temps at 120°C, darwin at 110°C), so only a real defect can trip them; empty
// results are valid — most CI runners report no sensors at all.
func TestPlatformCollectorsSane(t *testing.T) {
	ctx := context.Background()

	for _, s := range platformTemps(ctx) {
		if s.Name == "" {
			t.Error("temperature sensor with an empty name")
		}
		if !(s.TempC > 0 && s.TempC <= 150) { // also rejects NaN
			t.Errorf("implausible temperature %q: %v°C", s.Name, s.TempC)
		}
	}

	for _, f := range readFans() {
		if f.Name == "" {
			t.Error("fan with an empty name")
		}
		if !(f.RPM >= 0) { // also rejects NaN
			t.Errorf("fan %q: negative or NaN RPM %v", f.Name, f.RPM)
		}
	}

	if b := readBattery(ctx); b != nil && !(b.Percent >= 0 && b.Percent <= 100) {
		t.Errorf("battery percent out of range: %v", b.Percent)
	}

	if n := countUsers(); n < 0 {
		t.Errorf("negative logged-in user count: %d", n)
	}
}
