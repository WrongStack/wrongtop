package sensors

import (
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/theme"
	"github.com/wrongstack/wrongtop/internal/ui"
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

// hwSnapshot builds a snapshot with one reading per hardware group.
func hwSnapshot() collector.Snapshot {
	return collector.Snapshot{
		Time:    time.Now(),
		Sensors: []collector.Sensor{{Name: "CPU", TempC: 55}, {Name: "GPU", TempC: 48}},
		Fans:    []collector.Fan{{Name: "CPU", RPM: 1200}},
		Battery: &collector.Battery{Percent: 87, Charging: true},
	}
}

// TestTitle pins the tab label in both icon modes.
func TestTitle(t *testing.T) {
	cfg := config.Default()
	plain := New(cfg, theme.ByName(cfg.Theme)).Title()
	if !strings.Contains(plain, "SENSORS") {
		t.Errorf("plain title %q missing SENSORS", plain)
	}
	cfg.NerdFonts = true
	nerd := New(cfg, theme.ByName(cfg.Theme)).Title()
	if !strings.Contains(nerd, "SENSORS") || nerd == plain {
		t.Errorf("nerd title %q must keep the label and swap the glyph (plain %q)", nerd, plain)
	}
}

// TestSetVisibleIsNoop: history flows regardless of visibility, so
// toggling it changes nothing.
func TestSetVisibleIsNoop(t *testing.T) {
	m := newModel(t, 110, 34)
	m.Update(collector.SnapshotMsg{Snap: hwSnapshot()})
	before := m.View()
	m.SetVisible(false)
	m.SetVisible(true)
	if after := m.View(); after != before {
		t.Error("SetVisible must not change the rendered view")
	}
}

// TestSetSizeResizesGraphs: the rolling graphs track the terminal width
// within the clamp bounds.
func TestSetSizeResizesGraphs(t *testing.T) {
	m := newModel(t, 110, 34) // graphW = clamp(86, 16, 44) = 44
	m.Update(collector.SnapshotMsg{Snap: hwSnapshot()})
	g := m.tempHist["CPU"]
	if g == nil {
		t.Fatal("sensor graph not created")
	}
	if got, want := lipgloss.Width(g.View()), 44; got != want {
		t.Errorf("graph width = %d, want %d", got, want)
	}

	m.SetSize(40, 30) // clamp(16, 16, 44) = 16
	if got := m.graphW(); got != 16 {
		t.Errorf("graphW(40) = %d, want 16", got)
	}
	if got := lipgloss.Width(g.View()); got != 16 {
		t.Errorf("graph width after shrink = %d, want 16", got)
	}

	m.SetSize(10, 30) // clamped up to the 16-cell floor
	if got := m.graphW(); got != 16 {
		t.Errorf("graphW(10) = %d, want the 16-cell floor", got)
	}

	m.SetSize(300, 30) // clamped down to the 44-cell ceiling
	if got := m.graphW(); got != 44 {
		t.Errorf("graphW(300) = %d, want the 44-cell ceiling", got)
	}
	if got := lipgloss.Width(m.batGraph.View()); got != 44 {
		t.Errorf("battery graph width = %d, want 44", got)
	}
}

// TestUpdateIgnoresOtherMessages: only snapshot messages move the model.
func TestUpdateIgnoresOtherMessages(t *testing.T) {
	m := newModel(t, 110, 34)
	if cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); cmd != nil {
		t.Error("unhandled message must not return a command")
	}
	if m.live {
		t.Error("non-snapshot message must not mark the tab live")
	}
	if out := m.View(); !strings.Contains(out, "waiting for samples…") {
		t.Errorf("view must still wait for samples:\n%s", out)
	}
}

// TestViewWaitingForSamples pins the framed pre-snapshot state.
func TestViewWaitingForSamples(t *testing.T) {
	m := newModel(t, 110, 30)
	out := ui.StripANSI(m.View())
	if !strings.Contains(out, "waiting for samples…") || !strings.Contains(out, "SENSORS") {
		t.Errorf("fresh view missing the framed waiting state:\n%s", out)
	}
}

// TestViewZeroWidth: without a size the tab renders nothing.
func TestViewZeroWidth(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	if out := m.View(); out != "" {
		t.Errorf("zero-width view must be empty, got %q", out)
	}
}

// TestViewDropsGraphsWhenTight: on a short terminal the rolling history
// blocks are traded for bare readings.
func TestViewDropsGraphsWhenTight(t *testing.T) {
	m := newModel(t, 110, 60)
	m.Update(collector.SnapshotMsg{Snap: hwSnapshot()})
	tall := ui.StripANSI(m.View())

	m.SetSize(110, 8)
	tight := ui.StripANSI(m.View())

	if got := strings.Count(tight, "\n") + 1; got >= strings.Count(tall, "\n")+1 {
		t.Errorf("tight view (%d lines) should drop the graphs and shrink vs %d lines", got, strings.Count(tall, "\n")+1)
	}
	if !strings.Contains(tight, "55.0°C") {
		t.Errorf("tight view lost the temperature readings:\n%s", tight)
	}
}

// TestBatteryStates pins both charge states.
func TestBatteryStates(t *testing.T) {
	m := newModel(t, 110, 34)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time:    time.Now(),
		Battery: &collector.Battery{Percent: 42},
	}})
	out := ui.StripANSI(m.View())
	for _, want := range []string{"POWER", "42%", "on battery"} {
		if !strings.Contains(out, want) {
			t.Errorf("battery view missing %q:\n%s", want, out)
		}
	}
}

// TestBatteryOnlySnapshot: a machine with only a battery renders just the
// POWER section.
func TestBatteryOnlySnapshot(t *testing.T) {
	m := newModel(t, 110, 34)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time:    time.Now(),
		Battery: &collector.Battery{Percent: 50, Charging: true},
	}})
	out := ui.StripANSI(m.View())
	if !strings.Contains(out, "POWER") || !strings.Contains(out, "charging") {
		t.Errorf("battery-only view missing POWER:\n%s", out)
	}
	if strings.Contains(out, "TEMPERATURES") || strings.Contains(out, "COOLING") {
		t.Errorf("battery-only view rendered absent sections:\n%s", out)
	}
}

// TestSectionEmptyBody: an empty body renders no box at all.
func TestSectionEmptyBody(t *testing.T) {
	m := newModel(t, 110, 30)
	if got := m.section("sensor", "EMPTY", nil); got != "" {
		t.Errorf("empty section = %q, want \"\"", got)
	}
}

// TestAccentColors pins the per-section accent mapping plus the fallback.
func TestAccentColors(t *testing.T) {
	m := newModel(t, 110, 30)
	cases := map[string]string{
		"sensor": m.th.Palette.Orange,
		"fan":    m.th.Palette.Green,
		"bat":    m.th.Palette.Cyan,
		"gpu":    m.th.Palette.FG, // unknown kind → foreground
	}
	for kind, want := range cases {
		if got := m.accent(kind); got != want {
			t.Errorf("accent(%q) = %q, want %q", kind, got, want)
		}
	}
}

// TestFanNameFallback: unnamed fans get a stable positional key so their
// history survives across snapshots.
func TestFanNameFallback(t *testing.T) {
	if got := fanName("", 2); got != "F2" {
		t.Errorf("fanName(\"\", 2) = %q, want F2", got)
	}
	if got := fanName("CPU", 0); got != "CPU" {
		t.Errorf("fanName(\"CPU\", 0) = %q, want CPU", got)
	}

	m := newModel(t, 110, 34)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time: time.Now(),
		Fans: []collector.Fan{{Name: "", RPM: 2400}},
	}})
	out := ui.StripANSI(m.View())
	for _, want := range []string{"F0", "2400"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
}

// TestFanHistoryTrims: per-fan rpm history is bounded at fanSamples.
func TestFanHistoryTrims(t *testing.T) {
	m := newModel(t, 110, 34)
	for i := 0; i < fanSamples+3; i++ {
		m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
			Time: time.Now(),
			Fans: []collector.Fan{{Name: "CPU", RPM: float64(1000 + i)}},
		}})
	}
	if got := len(m.fanHist["CPU"]); got != fanSamples {
		t.Errorf("fan history length = %d, want %d", got, fanSamples)
	}
}
