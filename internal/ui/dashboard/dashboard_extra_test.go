package dashboard

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/theme"
)

func key(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: []rune(s)[0], Text: s} }

// New maps the configured layout onto the initial density preset.
func TestNewDensityFromLayout(t *testing.T) {
	cases := map[string]int{
		"full":     densityFull,
		"compact":  densityCompact,
		"minimal":  densityMinimal,
		"nonsense": densityFull, // unknown layouts fall back to full
	}
	for layout, want := range cases {
		cfg := config.Default()
		cfg.Layout = layout
		if got := New(cfg, theme.ByName(cfg.Theme)).density; got != want {
			t.Errorf("layout %q: density = %d, want %d", layout, got, want)
		}
	}
}

func TestTitleAndSetVisible(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	if got := m.Title(); got != "⌂ DASHBOARD" {
		t.Errorf("plain title = %q, want %q", got, "⌂ DASHBOARD")
	}
	m.cfg.NerdFonts = true
	if got := m.Title(); got != "\uf015 DASHBOARD" {
		t.Errorf("nerd title = %q, want the nerd home glyph", got)
	}
	m.SetVisible(false) // must be a no-op: the dashboard stays warm
	m.SetVisible(true)
}

// SetTheme swaps the palette and rebuilds every graph ramp, including
// the per-core graphs created from snapshots.
func TestSetThemeRebuildsRamps(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(120, 38)
	m.Update(collector.SnapshotMsg{Snap: fakeSnapshot()})
	if len(m.coreGraphs) == 0 {
		t.Fatal("expected per-core graphs after a snapshot")
	}
	m.View() // prime the cache

	nord := theme.ByName("nord")
	m.SetTheme(nord)
	if m.viewCache != "" {
		t.Error("SetTheme must invalidate the view cache")
	}
	if m.th != nord {
		t.Error("SetTheme must swap the theme")
	}
	if out := m.View(); !strings.Contains(out, "testhost") {
		t.Errorf("view should re-render after a theme swap: %q", out[:40])
	}
}

// SetSize ignores nonsense sizes and resizes existing per-core graphs.
func TestSetSizeEdges(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(0, 38)
	if m.width != 0 {
		t.Errorf("width = %d, want 0 (degenerate size stored as-is)", m.width)
	}
	m.SetSize(120, 38)
	m.Update(collector.SnapshotMsg{Snap: fakeSnapshot()})
	if len(m.coreGraphs) == 0 {
		t.Fatal("expected per-core graphs after a snapshot")
	}
	m.SetSize(120, 38) // resize again now that core graphs exist
	if out := m.View(); !strings.Contains(out, "CPU") {
		t.Error("resize should keep the dashboard renderable")
	}
}

// Update consumes snapshots and the density-cycle key.
func TestUpdateCyclesDensity(t *testing.T) {
	m := newModel(t, 120, 38)
	if m.density != densityFull {
		t.Fatalf("starting density = %d, want full", m.density)
	}
	m.Update(key("p"))
	if m.density != densityCompact || m.viewCache != "" {
		t.Error("p should cycle to compact and drop the cache")
	}
	m.Update(key("p"))
	m.Update(key("p"))
	if m.density != densityFull {
		t.Errorf("three presses should wrap to full, got %d", m.density)
	}
	m.Update(key("x"))
	if m.density != densityFull {
		t.Error("other keys must not change the density")
	}
	if cmd := m.Update(key("q")); cmd != nil {
		t.Error("dashboard key handling never returns a command")
	}
	var other tea.Msg = tea.MouseClickMsg{}
	if cmd := m.Update(other); cmd != nil {
		t.Error("non-key messages must be ignored")
	}
}

// recordIO keeps a bounded per-device history and resets itself when a
// machine churns device names.
func TestRecordIOTrimsAndResets(t *testing.T) {
	m := newModel(t, 100, 30)
	snap := fakeSnapshot()
	snap.DiskIOs = []collector.DiskIO{
		{Name: "disk1", ReadBytes: 100, WriteBytes: 200},
		{Name: "disk2", ReadBytes: 1, WriteBytes: 2},
	}
	for range 12 { // history caps at ioSparkSamples
		m.Update(collector.SnapshotMsg{Snap: snap})
	}
	if got := len(m.ioHist["disk1"]); got != ioSparkSamples {
		t.Errorf("ioHist[disk1] length = %d, want %d", got, ioSparkSamples)
	}
	if got := len(m.ioHist["disk2"]); got != ioSparkSamples {
		t.Errorf("ioHist[disk2] length = %d, want %d", got, ioSparkSamples)
	}

	for i := 0; i < 129; i++ { // churn past the map bound
		m.ioHist[fmt.Sprintf("usb-%d", i)] = nil
	}
	m.Update(collector.SnapshotMsg{Snap: snap})
	if got := len(m.ioHist); got != 2 {
		t.Errorf("ioHist should reset to just the live devices, has %d", got)
	}
}

// recordCores creates graphs on demand and prunes vanished cores.
func TestRecordCoresPrunes(t *testing.T) {
	m := newModel(t, 120, 38)
	snap4 := fakeSnapshot()
	snap4.CPU.Cores = []float64{10, 20, 30, 40}
	m.Update(collector.SnapshotMsg{Snap: snap4})
	if got := len(m.coreGraphs); got != 4 {
		t.Fatalf("core graphs = %d, want 4", got)
	}
	snap2 := fakeSnapshot()
	snap2.CPU.Cores = []float64{10, 20}
	m.Update(collector.SnapshotMsg{Snap: snap2})
	if got := len(m.coreGraphs); got != 2 {
		t.Errorf("core graphs after the core count dropped = %d, want 2", got)
	}
}

func TestViewCacheAndGuards(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))

	m.SetSize(0, 0)
	if got := m.View(); got != "" {
		t.Errorf("View at size 0x0 = %q, want \"\"", got)
	}

	m.SetSize(120, 38)
	wait := m.View()
	if !strings.Contains(wait, "waiting for samples") {
		t.Errorf("cold dashboard should show the wait box, got %q", wait)
	}

	m.Update(collector.SnapshotMsg{Snap: fakeSnapshot()})
	first := m.View()
	if first == wait || first == "" {
		t.Fatalf("live view should replace the wait box")
	}
	if second := m.View(); second != first {
		t.Error("View without new data must serve the cache")
	}
}

// The minimal preset trims the grid down to HOST and CPU.
func TestMinimalGridDropsLowerPanels(t *testing.T) {
	cfg := config.Default()
	cfg.Layout = "minimal"
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(120, 38)
	m.Update(collector.SnapshotMsg{Snap: fakeSnapshot()})
	m.Update(collector.SnapshotMsg{Snap: fakeSnapshot()})
	out := m.View()
	if !strings.Contains(out, "HOST") || !strings.Contains(out, "CPU") {
		t.Error("minimal grid should keep HOST and CPU")
	}
	for _, gone := range []string{"MEMORY", "PROCESSES"} {
		if strings.Contains(out, gone) {
			t.Errorf("minimal grid should drop %s", gone)
		}
	}
}

// The minimal preset also trims the stacked layout.
func TestMinimalStackedDropsLowerPanels(t *testing.T) {
	cfg := config.Default()
	cfg.Layout = "minimal"
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(100, 30)
	m.Update(collector.SnapshotMsg{Snap: fakeSnapshot()})
	m.Update(collector.SnapshotMsg{Snap: fakeSnapshot()})
	out := m.View()
	if !strings.Contains(out, "HOST") || !strings.Contains(out, "CPU") {
		t.Error("minimal stacked view should keep HOST and CPU")
	}
	if strings.Contains(out, "NETWORK") {
		t.Error("minimal stacked view should drop NETWORK")
	}
}

// The grid inserts a GPU band above the process panel when adapters are
// present.
func TestGridGPUBand(t *testing.T) {
	m := newModel(t, 120, 38)
	snap := fakeSnapshot()
	snap.GPUs = []collector.GPU{
		{Index: 0, Name: "Apple M2", Util: 55, MemUsed: 4 << 30, MemTotal: 10 << 30, TempC: 62},
		{Index: 1, Name: "Radeon", Util: 12, TempC: 55},
	}
	m.Update(collector.SnapshotMsg{Snap: snap})
	out := m.View()
	for _, want := range []string{"GPUS", "GPU0", "Apple M2", "2 adapters"} {
		if !strings.Contains(out, want) {
			t.Errorf("grid view missing %q", want)
		}
	}
	if got := strings.Count(out, "\n") + 1; got > 38 {
		t.Errorf("GPU band pushed the view to %d lines, max 38", got)
	}
}

// The stacked layout shows a GPU box when the row budget allows.
func TestStackedGPUBox(t *testing.T) {
	m := newModel(t, 100, 50)
	snap := fakeSnapshot()
	snap.GPUs = []collector.GPU{{Index: 0, Name: "Apple M2", Util: 55, TempC: 62}}
	m.Update(collector.SnapshotMsg{Snap: snap})
	out := m.View()
	if !strings.Contains(out, "GPUS") || !strings.Contains(out, "Apple M2") {
		t.Errorf("stacked view missing the GPU box:\n%s", out)
	}
}

// Short grids cannot pay for the process band: the data bands absorb the
// height and the process (and GPU) panel is dropped.
func TestGridPanelsShortGrid(t *testing.T) {
	m := newModel(t, 120, 38)
	snap := fakeSnapshot()
	snap.GPUs = []collector.GPU{{Index: 0, Name: "Apple M2"}}
	m.Update(collector.SnapshotMsg{Snap: snap})

	panels := m.gridPanels(15) // c lands below the process panel's floor
	var titles []string
	height := 0
	for _, p := range panels {
		titles = append(titles, p.Title)
		if top := p.Y + p.H; top > height {
			height = top
		}
	}
	joined := strings.Join(titles, " | ")
	if strings.Contains(joined, "PROCESSES") || strings.Contains(joined, "GPUS") {
		t.Errorf("a 15-row grid must drop the process and GPU panels: %v", titles)
	}
	for _, want := range []string{"HOST", "CPU", "MEMORY", "NETWORK", "DISKS"} {
		if !strings.Contains(joined, want) {
			t.Errorf("short grid missing %s: %v", want, titles)
		}
	}
	if height > 15 {
		t.Errorf("short grid panels span %d rows, max 15", height)
	}
}

func TestFitRight(t *testing.T) {
	if got := fitRight(20, " HOST ", "62%"); got != "62%" {
		t.Errorf("fitting readout = %q, want \"62%%\"", got)
	}
	if got := fitRight(10, " HOST ", "62%"); got != "" {
		t.Errorf("crowded readout = %q, want \"\"", got)
	}
	if got := fitRight(20, " HOST ", ""); got != "" {
		t.Errorf("empty readout = %q, want \"\"", got)
	}
}

// The box helper drops the border readout rather than stretching the box.
func TestBoxDropsCrowdedReadout(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	out := m.box(14, "host", "HOST", "up 72h", "content")
	if strings.Contains(out, "up 72h") {
		t.Errorf("crowded readout should be dropped: %q", out)
	}
	if !strings.Contains(out, "HOST") || !strings.Contains(out, "content") {
		t.Errorf("box lost its chrome or content: %q", out)
	}
}

// fitSnapshot returns a fully populated snapshot: OS name, users,
// battery, fans and a second sensor.
func richSnapshot() collector.Snapshot {
	snap := fakeSnapshot()
	snap.Time = time.Now()
	snap.Host.OS = "macOS 15.6"
	snap.Host.Users = 3
	snap.Sensors = []collector.Sensor{{Name: "CPU", TempC: 55}, {Name: "GPU", TempC: 48}, {Name: "PCH", TempC: 41}}
	snap.Battery = &collector.Battery{Percent: 77, Charging: true}
	snap.Fans = []collector.Fan{{Name: "CPU", RPM: 1200}, {Name: "GPU", RPM: 940}}
	return snap
}
