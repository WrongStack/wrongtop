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

// TestCollectSlowCaches verifies the slow-metric cache: the first call
// samples, calls within the interval reuse cached values (observable as an
// unchanged battery pointer), and the cache timestamp advances after the
// interval.
func TestCollectSlowCaches(t *testing.T) {
	c := New(time.Second)

	now := time.Now()
	f1, s1, b1 := c.collectSlow(context.Background(), now)
	f2, s2, b2 := c.collectSlow(context.Background(), now.Add(time.Second))
	if f1 != f2 || !sensorsEqual(s1, s2) || b1 != b2 {
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
