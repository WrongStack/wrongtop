package sensors

import (
	"strings"
	"testing"
	"time"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/theme"
)

func newModel(t *testing.T, w, h int) *Model {
	t.Helper()
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(w, h)
	return m
}

// TestViewRendersSections pins the three hardware groups: temperature
// graphs, fan rows and the battery block.
func TestViewRendersSections(t *testing.T) {
	m := newModel(t, 110, 34)
	for range 3 {
		m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
			Time: time.Now(),
			Host: collector.Host{Hostname: "mbp"},
			Sensors: []collector.Sensor{
				{Name: "CPU", TempC: 55},
				{Name: "GPU", TempC: 48},
			},
			Fans:    []collector.Fan{{Name: "CPU", RPM: 1200}, {Name: "GPU", RPM: 940}},
			Battery: &collector.Battery{Percent: 87, Charging: true},
		}})
	}
	out := m.View()
	for _, want := range []string{
		"TEMPERATURES", "COOLING", "POWER",
		"CPU", "GPU", "55.0°C", "48.0°C", "1200", "940", "87%", "charging",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
}

// TestEmptyState pins the graceful fallback when the platform reports
// no hardware readings at all.
func TestEmptyState(t *testing.T) {
	m := newModel(t, 110, 30)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{Time: time.Now()}})
	if out := m.View(); !strings.Contains(out, "no temperature, fan or battery") {
		t.Errorf("empty state missing:\n%s", out)
	}
}

// TestTempHistorySurvsThemeChange: live theme cycling rebuilds the graph
// ramps; existing per-sensor graphs must survive (same graph instances).
func TestTempHistorySurvsThemeChange(t *testing.T) {
	m := newModel(t, 110, 34)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time:    time.Now(),
		Sensors: []collector.Sensor{{Name: "CPU", TempC: 55}},
	}})
	g := m.tempHist["CPU"]
	if g == nil {
		t.Fatal("sensor graph not created")
	}
	m.SetTheme(theme.ByName("gruvbox-dark"))
	if m.tempHist["CPU"] != g {
		t.Error("theme change must keep the existing sensor graphs")
	}
}
