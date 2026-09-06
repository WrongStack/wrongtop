package disks

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/table"
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

func diskSnapshot() collector.Snapshot {
	return collector.Snapshot{
		Time: time.Now(),
		Disks: []collector.Disk{
			{Device: "/dev/disk3s1", Mountpoint: "/", FSType: "apfs",
				Total: 100 << 30, Used: 60 << 30, Free: 40 << 30, Percent: 60},
			{Device: "/dev/disk1s2", Mountpoint: "/private/var/vm", FSType: "apfs",
				Total: 4 << 30, Used: 1 << 30, Free: 3 << 30, Percent: 25},
		},
		DiskIOs: []collector.DiskIO{
			{Name: "disk3", ReadBytes: 1.2e6, WriteBytes: 3.4e5, ReadIOPS: 120, WriteIOPS: 40, BusyPercent: 12},
			{Name: "disk1", ReadBytes: 0, WriteBytes: 0, ReadIOPS: 0, WriteIOPS: 0, BusyPercent: 0},
		},
	}
}

// TestTitle pins the tab label in both icon modes.
func TestTitle(t *testing.T) {
	cfg := config.Default()
	plain := New(cfg, theme.ByName(cfg.Theme)).Title()
	if !strings.Contains(plain, "DISKS") {
		t.Errorf("plain title %q missing DISKS", plain)
	}
	cfg.NerdFonts = true
	nerd := New(cfg, theme.ByName(cfg.Theme)).Title()
	if !strings.Contains(nerd, "DISKS") || nerd == plain {
		t.Errorf("nerd title %q must keep the label and swap the glyph (plain %q)", nerd, plain)
	}
}

// TestSetTheme: live theme swaps restyle both tables without losing rows.
func TestSetTheme(t *testing.T) {
	m := newModel(t, 120, 40)
	m.Update(collector.SnapshotMsg{Snap: diskSnapshot()})
	m.SetTheme(theme.ByName("gruvbox-dark"))
	out := ui.StripANSI(m.View())
	for _, want := range []string{"FILESYSTEMS", "I/O RATES", "disk3s1"} {
		if !strings.Contains(out, want) {
			t.Errorf("view after theme swap missing %q:\n%s", want, out)
		}
	}
}

// TestUsageMode pins the FILESYSTEMS column tiers: TYPE drops first, then
// USED/TOTAL.
func TestUsageMode(t *testing.T) {
	cases := map[int]int{120: 2, 100: 2, 99: 1, 76: 1, 75: 0, 0: 0}
	for width, want := range cases {
		if got := usageMode(width); got != want {
			t.Errorf("usageMode(%d) = %d, want %d", width, got, want)
		}
	}
}

// TestColumnPlans checks the header plans directly: which columns exist
// at which width.
func TestColumnPlans(t *testing.T) {
	titles := func(cols []table.Column) []string {
		out := make([]string, len(cols))
		for i, c := range cols {
			out[i] = c.Title
		}
		return out
	}
	wantUsage := map[int][]string{
		120: {"DEVICE", "MOUNT", "TYPE", "USAGE", "USED", "TOTAL", "USE%"},
		90:  {"DEVICE", "MOUNT", "USAGE", "USE%"},
		60:  {"DEVICE", "MOUNT", "USAGE", "USE%"},
	}
	for width, want := range wantUsage {
		if got := titles(usageColumns(width)); !equalSlices(got, want) {
			t.Errorf("usageColumns(%d) = %v, want %v", width, got, want)
		}
	}
	wantIO := map[int][]string{
		120: {"DEVICE", "READ/s", "WRITE/s", "ACTIVITY", "R IOPS", "W IOPS", "BUSY"},
		90:  {"DEVICE", "READ/s", "WRITE/s", "ACTIVITY", "R IOPS", "W IOPS", "BUSY"},
		80:  {"DEVICE", "READ/s", "WRITE/s", "R IOPS", "W IOPS", "BUSY"},
		60:  {"DEVICE", "READ/s", "WRITE/s", "BUSY"},
	}
	for width, want := range wantIO {
		if got := titles(ioColumns(width, width >= 90)); !equalSlices(got, want) {
			t.Errorf("ioColumns(%d) = %v, want %v", width, got, want)
		}
	}
	// the activity flag is independent of the width tiers
	if got, want := titles(ioColumns(60, true)),
		[]string{"DEVICE", "READ/s", "WRITE/s", "ACTIVITY", "BUSY"}; !equalSlices(got, want) {
		t.Errorf("ioColumns(60, true) = %v, want %v", got, want)
	}
}

// TestSetSizeColumnShapes: the installed table headers must follow the
// width tiers after SetSize.
func TestSetSizeColumnShapes(t *testing.T) {
	cases := map[int][2]int{
		120: {7, 7},
		100: {7, 7},
		99:  {4, 7},
		90:  {4, 7},
		80:  {4, 6},
		60:  {4, 4},
	}
	for width, want := range cases {
		m := newModel(t, width, 40)
		if got := len(m.table.Columns()); got != want[0] {
			t.Errorf("width %d: fs table has %d columns, want %d", width, got, want[0])
		}
		if got := len(m.io.Columns()); got != want[1] {
			t.Errorf("width %d: io table has %d columns, want %d", width, got, want[1])
		}
	}
}

// TestSetSizeTinyTerminal: even a 0x0 terminal keeps positive table
// heights instead of collapsing the viewports.
func TestSetSizeTinyTerminal(t *testing.T) {
	m := newModel(t, 0, 0)
	if m.table.Height() < 1 || m.io.Height() < 1 {
		t.Errorf("tiny terminal collapsed the viewports: fs %d io %d", m.table.Height(), m.io.Height())
	}
	if out := m.View(); out != "" {
		t.Errorf("zero-width view must be empty, got %q", out)
	}
}

// TestShowActivity pins the sparkline column threshold.
func TestShowActivity(t *testing.T) {
	m := newModel(t, 90, 30)
	if !m.showActivity() {
		t.Error("90 columns should leave room for ACTIVITY")
	}
	m.SetSize(89, 30)
	if m.showActivity() {
		t.Error("89 columns should drop ACTIVITY")
	}
}

// TestViewRendersBothTables pins the full-width view: headers, filesystem
// rows, and I/O rows with rates and IOPS. (The fs table's USED/TOTAL/USE%
// data cells render past the viewport clip — only their headers show — so
// they are asserted as headers.)
func TestViewRendersBothTables(t *testing.T) {
	m := newModel(t, 120, 40)
	m.Update(collector.SnapshotMsg{Snap: diskSnapshot()})
	out := ui.StripANSI(m.View())
	for _, want := range []string{
		"FILESYSTEMS", "I/O RATES", "←→ table",
		"DEVICE", "MOUNT", "TYPE", "USAGE", "USED", "TOTAL", "USE%",
		"disk3s1", "apfs",
		"ACTIVITY", "R IOPS", "W IOPS", "BUSY",
		"disk3", "1.2 Mb/s", "340.0 Kb/s", "120", "12%",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

// TestViewNarrowDropsColumns: shrinking the terminal must remove the
// matching headers from the rendered table.
func TestViewNarrowDropsColumns(t *testing.T) {
	m := newModel(t, 90, 40)
	m.Update(collector.SnapshotMsg{Snap: diskSnapshot()})
	out := ui.StripANSI(m.View())
	if strings.Contains(out, "TYPE") || strings.Contains(out, "TOTAL") {
		t.Errorf("90-column view keeps wide fs columns:\n%s", out)
	}
	if !strings.Contains(out, "ACTIVITY") {
		t.Error("90-column view should keep ACTIVITY")
	}

	m = newModel(t, 60, 30)
	m.Update(collector.SnapshotMsg{Snap: diskSnapshot()})
	out = ui.StripANSI(m.View())
	if strings.Contains(out, "IOPS") || strings.Contains(out, "ACTIVITY") {
		t.Errorf("60-column view keeps wide io columns:\n%s", out)
	}
	if !strings.Contains(out, "disk3") {
		t.Errorf("60-column view lost the io rows:\n%s", out)
	}
}

// TestViewWaitingForSamples pins the pre-snapshot hint.
func TestViewWaitingForSamples(t *testing.T) {
	m := newModel(t, 120, 40)
	if out := m.View(); !strings.Contains(out, "waiting for samples…") {
		t.Errorf("fresh view missing the waiting hint:\n%s", out)
	}
}

// TestFocusFollowsArrowKeys: left/right move keyboard control between
// the two tables, up/down scroll the focused one.
func TestFocusFollowsArrowKeys(t *testing.T) {
	m := newModel(t, 120, 40)
	m.Update(collector.SnapshotMsg{Snap: diskSnapshot()})
	m.table.SetCursor(0) // normalize: row clears park the bubbles cursor at -1

	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.table.Cursor(); got != 1 {
		t.Fatalf("down: fs cursor = %d, want 1", got)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.table.Cursor(); got != 0 {
		t.Fatalf("up: fs cursor = %d, want 0", got)
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.focus != 1 {
		t.Fatalf("left: focus = %d, want 1", m.focus)
	}
	out := ui.StripANSI(m.View())
	if !strings.Contains(out, "FILESYSTEMS") || !strings.Contains(out, "I/O RATES") {
		t.Fatalf("focused-io view lost a table header:\n%s", out)
	}
	m.io.SetCursor(0) // normalize: row clears park the bubbles cursor at -1
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.io.Cursor(); got != 1 {
		t.Fatalf("down on io table: cursor = %d, want 1", got)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.focus != 0 {
		t.Fatalf("right: focus = %d, want 0", m.focus)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.table.Cursor(); got != 0 {
		t.Fatalf("up after refocus: fs cursor = %d, want 0", got)
	}
}

// TestMouseWheelRoutesByPointerY: the wheel scrolls whichever table sits
// under the pointer.
func TestMouseWheelRoutesByPointerY(t *testing.T) {
	m := newModel(t, 120, 40)
	m.Update(collector.SnapshotMsg{Snap: diskSnapshot()})

	ioY := 6 + m.table.Height() + 1 // below the io table header

	m.table.SetCursor(1)
	m.Update(tea.MouseWheelMsg{Y: 2, Button: tea.MouseWheelUp})
	if got := m.table.Cursor(); got != 0 {
		t.Errorf("wheel up over fs table: cursor = %d, want 0", got)
	}
	m.Update(tea.MouseWheelMsg{Y: 2, Button: tea.MouseWheelDown})
	if got := m.table.Cursor(); got != 1 {
		t.Errorf("wheel down over fs table: cursor = %d, want 1", got)
	}

	m.Update(tea.MouseWheelMsg{Y: ioY, Button: tea.MouseWheelDown})
	if got := m.io.Cursor(); got != 1 {
		t.Errorf("wheel down over io table: cursor = %d, want 1", got)
	}
	m.Update(tea.MouseWheelMsg{Y: ioY, Button: tea.MouseWheelUp})
	if got := m.io.Cursor(); got != 0 {
		t.Errorf("wheel up over io table: cursor = %d, want 0", got)
	}
}

// TestMouseClickSelectsRow: clicks land on the row under the pointer and
// move the focus; right-clicks and dead zones do nothing.
func TestMouseClickSelectsRow(t *testing.T) {
	m := newModel(t, 120, 40)
	m.Update(collector.SnapshotMsg{Snap: diskSnapshot()})

	m.Update(tea.MouseClickMsg{Y: 4, Button: tea.MouseLeft}) // second fs data row
	if got := m.table.Cursor(); got != 1 {
		t.Errorf("click on second fs row: cursor = %d, want 1", got)
	}
	if m.focus != 0 {
		t.Errorf("click on fs table: focus = %d, want 0", m.focus)
	}

	ioY := 6 + m.table.Height() + 1 // second io data row
	m.Update(tea.MouseClickMsg{Y: ioY, Button: tea.MouseLeft})
	if got := m.io.Cursor(); got != 1 {
		t.Errorf("click on second io row: cursor = %d, want 1", got)
	}
	if m.focus != 1 {
		t.Errorf("click on io table: focus = %d, want 1", m.focus)
	}

	m.Update(tea.MouseClickMsg{Y: 4, Button: tea.MouseRight})
	if got := m.io.Cursor(); got != 1 {
		t.Errorf("right click must be ignored: cursor = %d, want 1", got)
	}

	m.Update(tea.MouseClickMsg{Y: 5 + m.table.Height(), Button: tea.MouseLeft}) // gap between tables
	if got := m.io.Cursor(); got != 1 {
		t.Errorf("click between the tables must be ignored: cursor = %d, want 1", got)
	}
}

// TestMouseClickEmptyTables: clicking a fresh, rowless tab must not move
// any cursor.
func TestMouseClickEmptyTables(t *testing.T) {
	m := newModel(t, 120, 40)
	fs, io := m.table.Cursor(), m.io.Cursor()
	m.Update(tea.MouseClickMsg{Y: 3, Button: tea.MouseLeft})
	if m.table.Cursor() != fs || m.io.Cursor() != io {
		t.Errorf("click without rows moved a cursor: fs %d io %d", m.table.Cursor(), m.io.Cursor())
	}
}

// TestSetVisibleDefersRebuild: hidden tabs keep history but skip the row
// rebuild; activation catches up from the latest snapshot.
func TestSetVisibleDefersRebuild(t *testing.T) {
	m := newModel(t, 120, 40)
	m.SetVisible(false)
	m.Update(collector.SnapshotMsg{Snap: diskSnapshot()})
	if got := len(m.table.Rows()); got != 0 {
		t.Errorf("hidden tab rebuilt %d fs rows, want 0", got)
	}
	m.SetVisible(true)
	if got := len(m.table.Rows()); got != 2 {
		t.Errorf("activated tab has %d fs rows, want 2", got)
	}
	if got := len(m.io.Rows()); got != 2 {
		t.Errorf("activated tab has %d io rows, want 2", got)
	}
	out := ui.StripANSI(m.View())
	if !strings.Contains(out, "disk3s1") {
		t.Errorf("activated view missing fs rows:\n%s", out)
	}
}

// TestRecordIOTrimsHistory: per-device sparkline history is bounded at
// activitySamples.
func TestRecordIOTrimsHistory(t *testing.T) {
	m := newModel(t, 120, 40)
	for i := 0; i < activitySamples+3; i++ {
		m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
			DiskIOs: []collector.DiskIO{{Name: "sda", ReadBytes: float64(i)}},
		}})
	}
	if got := len(m.hist["sda"]); got != activitySamples {
		t.Errorf("history length = %d, want %d", got, activitySamples)
	}
}

// TestRecordIOBoundsTheMap: with more distinct devices than the map
// bound, the history resets instead of growing unboundedly.
func TestRecordIOBoundsTheMap(t *testing.T) {
	m := newModel(t, 120, 40)
	m.SetVisible(false) // exercise the history without 150-row rebuilds
	devs := make([]collector.DiskIO, 0, 150)
	for i := range 150 {
		devs = append(devs, collector.DiskIO{Name: fmt.Sprintf("sd%03d", i)})
	}
	msg := collector.SnapshotMsg{Snap: collector.Snapshot{DiskIOs: devs}}
	m.Update(msg)
	m.Update(msg) // second pass crosses the 128-key bound
	if len(m.hist) != len(devs) {
		t.Errorf("history map holds %d devices, want %d", len(m.hist), len(devs))
	}
	if got := len(m.hist["sd000"]); got != 1 {
		t.Errorf("after the bound reset sd000 has %d samples, want 1 (fresh)", got)
	}
}

// TestUnknownMessageIgnored: messages outside the handled set change
// nothing.
func TestUnknownMessageIgnored(t *testing.T) {
	m := newModel(t, 120, 40)
	if cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24}); cmd != nil {
		t.Error("unhandled message must not return a command")
	}
	if m.width != 120 {
		t.Errorf("width = %d, want 120 (only SetSize owns size)", m.width)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
