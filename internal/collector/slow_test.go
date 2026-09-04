package collector

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCollectSensorsFiltersCPURelevant(t *testing.T) {
	for _, s := range collectSensors(context.Background()) {
		if smcStyleName(s.Name) {
			continue // AppleSMC keys are pre-filtered by key family
		}
		relevant := false
		for _, hint := range sensorHints {
			if strings.Contains(strings.ToLower(s.Name), hint) {
				relevant = true
				break
			}
		}
		if !relevant {
			t.Errorf("sensor %q passed the CPU filter unexpectedly", s.Name)
		}
		if s.TempC <= 0 {
			t.Errorf("sensor %q has non-positive temperature %v", s.Name, s.TempC)
		}
	}
}

// smcStyleName reports whether name looks like an AppleSMC temperature
// key (Tp01, Te05, TC0P, …) as emitted by platformTemps on darwin.
func smcStyleName(name string) bool {
	return len(name) == 4 && name[0] == 'T' && strings.ContainsAny(name[1:2], "peC")
}

// TestCollectSlowCaches verifies the slow-metric cache: the first call
// samples, calls within the interval reuse cached values (observable as an
// unchanged battery pointer), and the cache timestamp advances after the
// interval.
func TestCollectSlowCaches(t *testing.T) {
	c := New(time.Second)

	now := time.Now()
	f1, s1, n1, b1 := c.collectSlow(context.Background(), now)
	f2, s2, n2, b2 := c.collectSlow(context.Background(), now.Add(time.Second))
	if f1 != f2 || !sensorsEqual(s1, s2) || !fansEqual(n1, n2) || b1 != b2 {
		t.Error("slow metrics were re-sampled within the cache interval")
	}

	c.collectSlow(context.Background(), now.Add(6*time.Second))
	if c.lastSlow != now.Add(6*time.Second) {
		t.Errorf("lastSlow not advanced: %v", c.lastSlow)
	}
}

func sensorsEqual(a, b []Sensor) bool {
	return slices.EqualFunc(a, b, func(x, y Sensor) bool {
		return x.Name == y.Name && x.TempC == y.TempC
	})
}

func fansEqual(a, b []Fan) bool {
	return slices.EqualFunc(a, b, func(x, y Fan) bool {
		return x.Name == y.Name && x.RPM == y.RPM
	})
}
