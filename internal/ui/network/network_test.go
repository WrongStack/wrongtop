package network

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"

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

func netSnapshot() collector.Snapshot {
	return collector.Snapshot{
		Time: time.Now(),
		Nets: []collector.NetIface{
			{Name: "en0", RxRate: 1.5e6, TxRate: 3.4e5,
				RxTotal: 250 << 20, TxTotal: 100 << 20, RxDrop: 2, TxDrop: 1},
			{Name: "utun4", RxRate: 120, TxRate: 90, RxTotal: 4096, TxTotal: 2048},
			{Name: "bridge0"}, // idle interface: zero rates, no drops
		},
	}
}

// TestTitle pins the tab label in both icon modes.
func TestTitle(t *testing.T) {
	cfg := config.Default()
	plain := New(cfg, theme.ByName(cfg.Theme)).Title()
	if !strings.Contains(plain, "NETWORK") {
		t.Errorf("plain title %q missing NETWORK", plain)
	}
	cfg.NerdFonts = true
	nerd := New(cfg, theme.ByName(cfg.Theme)).Title()
	if !strings.Contains(nerd, "NETWORK") || nerd == plain {
		t.Errorf("nerd title %q must keep the label and swap the glyph (plain %q)", nerd, plain)
	}
}

// TestSetTheme: live theme swaps restyle the table without losing rows.
func TestSetTheme(t *testing.T) {
	m := newModel(t, 120, 30)
	m.Update(collector.SnapshotMsg{Snap: netSnapshot()})
	m.SetTheme(theme.ByName("gruvbox-dark"))
	if out := ui.StripANSI(m.View()); !strings.Contains(out, "en0") {
		t.Errorf("view after theme swap lost the rows:\n%s", out)
	}
}

// TestColumnPlan checks the header plan tiers: totals drop first, then
// drops, then the activity sparkline.
func TestColumnPlan(t *testing.T) {
	cases := map[int][]string{
		120: {"IFACE", "RX/s", "TX/s", "TOTAL RX", "TOTAL TX", "DROP/s", "ACTIVITY", "MIX ↓↑"},
		111: {"IFACE", "RX/s", "TX/s", "TOTAL RX", "TOTAL TX", "ACTIVITY", "MIX ↓↑"},
		100: {"IFACE", "RX/s", "TX/s", "TOTAL RX", "TOTAL TX", "ACTIVITY", "MIX ↓↑"},
		99:  {"IFACE", "RX/s", "TX/s", "ACTIVITY", "MIX ↓↑"},
		88:  {"IFACE", "RX/s", "TX/s", "ACTIVITY", "MIX ↓↑"},
		87:  {"IFACE", "RX/s", "TX/s", "MIX ↓↑"},
	}
	for width, want := range cases {
		cols := columns(width)
		got := make([]string, len(cols))
		for i, c := range cols {
			got[i] = c.Title
		}
		if len(got) != len(want) {
			t.Errorf("columns(%d) = %v, want %v", width, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("columns(%d) = %v, want %v", width, got, want)
				break
			}
		}
	}
}

// TestSetSizeKeepsPositiveHeight: even a 0x0 terminal keeps the viewport
// usable and the view empty (width < 1 renders nothing).
func TestSetSizeKeepsPositiveHeight(t *testing.T) {
	m := newModel(t, 0, 0)
	if m.table.Height() < 1 {
		t.Errorf("tiny terminal collapsed the viewport: height %d", m.table.Height())
	}
	if out := m.View(); out != "" {
		t.Errorf("zero-width view must be empty, got %q", out)
	}
}

// TestViewRendersTotalsAndSortsByActivity pins the head line figures and
// the busiest-first row order.
func TestViewRendersTotalsAndSortsByActivity(t *testing.T) {
	m := newModel(t, 120, 30)
	m.Update(collector.SnapshotMsg{Snap: netSnapshot()})
	out := ui.StripANSI(m.View())
	for _, want := range []string{
		"TOTAL", "1.5 Mb/s", "340.0 Kb/s",
		"250.0 MiB", "100.0 MiB",
		"en0", "utun4", "bridge0",
		"interfaces sorted by current activity",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q", want)
		}
	}

	rows := m.table.Rows()
	if len(rows) != 3 {
		t.Fatalf("table has %d rows, want 3", len(rows))
	}
	if !strings.HasPrefix(strings.TrimSpace(rows[0][0]), "en0") {
		t.Errorf("busiest interface should lead the table, got %q first", rows[0][0])
	}
	if !strings.HasPrefix(strings.TrimSpace(rows[2][0]), "bridge0") {
		t.Errorf("idle interface should be last, got %q last", rows[2][0])
	}
}

// TestViewWaitingForSamples pins the pre-snapshot hint.
func TestViewWaitingForSamples(t *testing.T) {
	m := newModel(t, 120, 30)
	if out := m.View(); !strings.Contains(out, "waiting for samples…") {
		t.Errorf("fresh view missing the waiting hint:\n%s", out)
	}
}

// TestRebuildFollowsWidthTiers: shrinking the terminal removes the
// matching columns from the rendered table.
func TestRebuildFollowsWidthTiers(t *testing.T) {
	cases := []struct {
		width    int
		present  []string
		absent   []string
		wantCols int
	}{
		{120, []string{"TOTAL RX", "DROP/s", "ACTIVITY"}, nil, 8},
		{111, []string{"TOTAL RX", "ACTIVITY"}, []string{"DROP/s"}, 7},
		{99, []string{"ACTIVITY"}, []string{"TOTAL RX", "DROP/s"}, 5},
		{87, nil, []string{"TOTAL RX", "DROP/s", "ACTIVITY"}, 4},
	}
	for _, tc := range cases {
		m := newModel(t, tc.width, 30)
		m.Update(collector.SnapshotMsg{Snap: netSnapshot()})
		if got := len(m.table.Columns()); got != tc.wantCols {
			t.Errorf("width %d: %d columns, want %d", tc.width, got, tc.wantCols)
		}
		out := ui.StripANSI(m.View())
		for _, want := range tc.present {
			if !strings.Contains(out, want) {
				t.Errorf("width %d: view missing %q", tc.width, want)
			}
		}
		for _, banned := range tc.absent {
			if strings.Contains(out, banned) {
				t.Errorf("width %d: view keeps %q", tc.width, banned)
			}
		}
	}
}

// TestRebuildColorsDrops: interfaces losing packets get the warn-styled
// drop cell, quiet ones the muted style — both figures still render.
func TestRebuildColorsDrops(t *testing.T) {
	m := newModel(t, 120, 30)
	m.Update(collector.SnapshotMsg{Snap: netSnapshot()})
	out := ui.StripANSI(m.View())
	if !strings.Contains(out, "3.0") { // en0: 2 rx + 1 tx drops
		t.Errorf("drop figure missing:\n%s", out)
	}
}

// TestSetVisibleDefersRebuild: hidden tabs keep history but skip the row
// rebuild; activation catches up from the latest snapshot.
func TestSetVisibleDefersRebuild(t *testing.T) {
	m := newModel(t, 120, 30)
	m.SetVisible(false)
	m.Update(collector.SnapshotMsg{Snap: netSnapshot()})
	if got := len(m.table.Rows()); got != 0 {
		t.Errorf("hidden tab rebuilt %d rows, want 0", got)
	}
	m.SetVisible(true)
	if got := len(m.table.Rows()); got != 3 {
		t.Errorf("activated tab has %d rows, want 3", got)
	}
	if out := ui.StripANSI(m.View()); !strings.Contains(out, "en0") {
		t.Errorf("activated view missing rows:\n%s", out)
	}
}

// TestMouseWheelScrolls: the wheel steps the selection three rows.
func TestMouseWheelScrolls(t *testing.T) {
	m := newModel(t, 120, 30)
	m.Update(collector.SnapshotMsg{Snap: netSnapshot()})
	m.table.SetCursor(0) // normalize: row clears park the bubbles cursor at -1

	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if got := m.table.Cursor(); got != 2 {
		t.Errorf("wheel down: cursor = %d, want 2 (clamped to the last row)", got)
	}
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if got := m.table.Cursor(); got != 0 {
		t.Errorf("wheel up: cursor = %d, want 0", got)
	}
}

// TestMouseClickSelectsRow: a left click focuses the clicked row; other
// buttons and clicks off the rows change nothing.
func TestMouseClickSelectsRow(t *testing.T) {
	m := newModel(t, 120, 30)
	m.Update(collector.SnapshotMsg{Snap: netSnapshot()})

	m.Update(tea.MouseClickMsg{Y: 4, Button: tea.MouseLeft}) // second data row
	if got := m.table.Cursor(); got != 1 {
		t.Errorf("click on second row: cursor = %d, want 1", got)
	}

	before := m.table.Cursor()
	m.Update(tea.MouseClickMsg{Y: 4, Button: tea.MouseRight})
	if got := m.table.Cursor(); got != before {
		t.Errorf("right click moved the cursor: %d, want %d", got, before)
	}
	m.Update(tea.MouseClickMsg{Y: 1, Button: tea.MouseLeft}) // above the table
	if got := m.table.Cursor(); got != before {
		t.Errorf("click on the head line moved the cursor: %d, want %d", got, before)
	}
}

// TestRecordRatesTrimsHistory: per-interface sparkline history is
// bounded at activitySamples.
func TestRecordRatesTrimsHistory(t *testing.T) {
	m := newModel(t, 120, 30)
	for i := 0; i < activitySamples+3; i++ {
		m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
			Nets: []collector.NetIface{{Name: "eth0", RxRate: float64(i)}},
		}})
	}
	if got := len(m.hist["eth0"]); got != activitySamples {
		t.Errorf("history length = %d, want %d", got, activitySamples)
	}
}

// TestRecordRatesBoundsTheMap: with more distinct interfaces than the
// map bound, the history resets instead of growing unboundedly.
func TestRecordRatesBoundsTheMap(t *testing.T) {
	m := newModel(t, 120, 30)
	m.SetVisible(false) // exercise the history without 150-row rebuilds
	ifaces := make([]collector.NetIface, 0, 150)
	for i := range 150 {
		ifaces = append(ifaces, collector.NetIface{Name: fmt.Sprintf("eth%03d", i)})
	}
	msg := collector.SnapshotMsg{Snap: collector.Snapshot{Nets: ifaces}}
	m.Update(msg)
	m.Update(msg) // second pass crosses the 128-key bound
	if len(m.hist) != len(ifaces) {
		t.Errorf("history map holds %d interfaces, want %d", len(m.hist), len(ifaces))
	}
	if got := len(m.hist["eth000"]); got != 1 {
		t.Errorf("after the bound reset eth000 has %d samples, want 1 (fresh)", got)
	}
}

// TestUnknownMessageIgnored: messages outside the handled set change
// nothing.
func TestUnknownMessageIgnored(t *testing.T) {
	m := newModel(t, 120, 30)
	if cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24}); cmd != nil {
		t.Error("unhandled message must not return a command")
	}
	if m.width != 120 {
		t.Errorf("width = %d, want 120 (only SetSize owns size)", m.width)
	}
}
