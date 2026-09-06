package conns

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

// TestViewRendersHead pins the summary line: totals, established and
// listener counts with state coloring.
func TestViewRendersHead(t *testing.T) {
	m := newModel(t, 120, 30)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time: time.Now(),
		Conns: []collector.Conn{
			{Local: "10.0.0.1:5000", Remote: "1.1.1.1:443", State: "ESTABLISHED", PID: 42},
			{Local: "*:8080", State: "LISTEN", PID: 7},
			{Local: "10.0.0.1:5001", Remote: "1.1.1.1:80", State: "TIME_WAIT"},
		},
		Procs: []collector.Proc{{PID: 42, Name: "curl"}, {PID: 7, Name: "myserver"}},
	}})
	out := m.View()
	for _, want := range []string{"3 conns", "1 established", "1 listen", "ESTABLISHED", "LISTEN", "TIME_WAIT", "curl", "myserver"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
}

// TestEmptyState pins the centered placeholder when the table is empty.
func TestEmptyState(t *testing.T) {
	m := newModel(t, 120, 30)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{Time: time.Now()}})
	if out := m.View(); !strings.Contains(out, "no TCP connections") {
		t.Errorf("empty state missing:\n%s", out)
	}
}

// TestProcessColumnsFollowPIDAvailability: when no connection carries a
// PID (linux kernel tables) the PROCESS column must disappear — rows and
// headers rebuilt together.
func TestProcessColumnsFollowPIDAvailability(t *testing.T) {
	m := newModel(t, 120, 30)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time:  time.Now(),
		Conns: []collector.Conn{{Local: "a:b", State: "ESTABLISHED", PID: 42}},
		Procs: []collector.Proc{{PID: 42, Name: "x"}},
	}})
	if !strings.Contains(m.View(), "PROCESS") {
		t.Error("PROCESS column missing while PIDs resolve")
	}
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time:  time.Now(),
		Conns: []collector.Conn{{Local: "a:b", State: "ESTABLISHED"}},
	}})
	if strings.Contains(m.View(), "PROCESS") {
		t.Error("PROCESS column kept without PIDs")
	}
}

// TestTitle pins the tab label in both icon modes.
func TestTitle(t *testing.T) {
	cfg := config.Default()
	plain := New(cfg, theme.ByName(cfg.Theme)).Title()
	if !strings.Contains(plain, "CONNECTIONS") {
		t.Errorf("plain title %q missing CONNECTIONS", plain)
	}
	cfg.NerdFonts = true
	nerd := New(cfg, theme.ByName(cfg.Theme)).Title()
	if !strings.Contains(nerd, "CONNECTIONS") || nerd == plain {
		t.Errorf("nerd title %q must keep the label and swap the glyph (plain %q)", nerd, plain)
	}
}

// TestSetTheme: live theme swaps restyle the table without losing rows.
func TestSetTheme(t *testing.T) {
	m := newModel(t, 120, 30)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time:  time.Now(),
		Conns: []collector.Conn{{Local: "a:b", Remote: "c:d", State: "ESTABLISHED"}},
	}})
	m.SetTheme(theme.ByName("gruvbox-dark"))
	if out := ui.StripANSI(m.View()); !strings.Contains(out, "ESTABLISHED") {
		t.Errorf("view after theme swap lost the rows:\n%s", out)
	}
}

// TestSetVisibleDefersRebuild: hidden tabs keep the latest snapshot but
// skip the rebuild; activation catches up and brings the process column.
func TestSetVisibleDefersRebuild(t *testing.T) {
	m := newModel(t, 120, 30)
	m.SetVisible(false)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time:  time.Now(),
		Conns: []collector.Conn{{Local: "10.0.0.1:5000", Remote: "1.1.1.1:443", State: "ESTABLISHED", PID: 42}},
		Procs: []collector.Proc{{PID: 42, Name: "curl"}},
	}})
	if got := len(m.table.Rows()); got != 0 {
		t.Errorf("hidden tab rebuilt %d rows, want 0", got)
	}
	m.SetVisible(true)
	if got := len(m.table.Rows()); got != 1 {
		t.Errorf("activated tab has %d rows, want 1", got)
	}
	out := ui.StripANSI(m.View())
	for _, want := range []string{"10.0.0.1:5000", "curl"} {
		if !strings.Contains(out, want) {
			t.Errorf("activated view missing %q:\n%s", want, out)
		}
	}
}

// TestKeyNavigation: up/down move the table selection.
func TestKeyNavigation(t *testing.T) {
	m := newModel(t, 120, 30)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time: time.Now(),
		Conns: []collector.Conn{
			{Local: "a:1", State: "ESTABLISHED"},
			{Local: "a:2", State: "ESTABLISHED"},
			{Local: "a:3", State: "ESTABLISHED"},
		},
	}})
	m.table.SetCursor(0) // normalize: row clears park the bubbles cursor at -1

	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.table.Cursor(); got != 1 {
		t.Errorf("down: cursor = %d, want 1", got)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.table.Cursor(); got != 0 {
		t.Errorf("up: cursor = %d, want 0", got)
	}
}

// TestMouseWheelScrolls: the wheel steps the selection three rows.
func TestMouseWheelScrolls(t *testing.T) {
	m := newModel(t, 120, 30)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time: time.Now(),
		Conns: []collector.Conn{
			{Local: "a:1", State: "ESTABLISHED"},
			{Local: "a:2", State: "ESTABLISHED"},
			{Local: "a:3", State: "ESTABLISHED"},
		},
	}})
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
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time: time.Now(),
		Conns: []collector.Conn{
			{Local: "a:1", State: "ESTABLISHED"},
			{Local: "a:2", State: "ESTABLISHED"},
		},
	}})

	m.Update(tea.MouseClickMsg{Y: 4, Button: tea.MouseLeft}) // second data row
	if got := m.table.Cursor(); got != 1 {
		t.Errorf("click on second row: cursor = %d, want 1", got)
	}

	before := m.table.Cursor()
	m.Update(tea.MouseClickMsg{Y: 4, Button: tea.MouseRight})
	if got := m.table.Cursor(); got != before {
		t.Errorf("right click moved the cursor: %d, want %d", got, before)
	}
	m.Update(tea.MouseClickMsg{Y: 1, Button: tea.MouseLeft}) // on the head line
	if got := m.table.Cursor(); got != before {
		t.Errorf("click above the table moved the cursor: %d, want %d", got, before)
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

// TestLayoutForWidths pins the shared column plan tiers.
func TestLayoutForWidths(t *testing.T) {
	cases := []struct {
		width           int
		localW, remoteW int
		anyPID          bool
	}{
		{60, 20, 20, true},
		{83, 20, 20, false},
		{84, 22, 24, true},
		{107, 22, 24, true},
		{108, 24, 28, false},
		{200, 24, 28, true},
	}
	for _, tc := range cases {
		got := layoutFor(tc.width, tc.anyPID)
		if got.localW != tc.localW || got.remoteW != tc.remoteW || got.pid != tc.anyPID {
			t.Errorf("layoutFor(%d, %v) = %+v, want local %d remote %d pid %v",
				tc.width, tc.anyPID, got, tc.localW, tc.remoteW, tc.anyPID)
		}
	}
}

// TestPIDCellFallbacks: a resolved PID shows the process name, an
// unresolved one its number, and a connection without a PID a dash —
// while any PID exists at all.
func TestPIDCellFallbacks(t *testing.T) {
	m := newModel(t, 140, 30)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time: time.Now(),
		Conns: []collector.Conn{
			{Local: "a:1", Remote: "b:2", State: "ESTABLISHED", PID: 42},
			{Local: "a:2", Remote: "b:3", State: "ESTABLISHED", PID: 99},
			{Local: "a:3", State: "LISTEN"},
		},
		Procs: []collector.Proc{{PID: 42, Name: "curl"}},
	}})
	out := ui.StripANSI(m.View())
	for _, want := range []string{"curl", "99", "—"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
}

// TestStateStyleGroups: every TCP state family renders its state string.
func TestStateStyleGroups(t *testing.T) {
	states := []string{
		"ESTABLISHED", "LISTEN", "TIME_WAIT", "CLOSE",
		"CLOSE_WAIT", "LAST_ACK", "SYN_SENT", "SYN_RECV", "FIN_WAIT1", // FIN_WAIT1: unclassified → default style
	}
	conns := make([]collector.Conn, len(states))
	for i, s := range states {
		conns[i] = collector.Conn{Local: fmt.Sprintf("10.0.0.1:%d", i+1), State: s}
	}
	m := newModel(t, 160, 40)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{Time: time.Now(), Conns: conns}})
	out := ui.StripANSI(m.View())
	for _, s := range states {
		if !strings.Contains(out, s) {
			t.Errorf("view missing state %q", s)
		}
	}
}

// TestViewBeforeFirstSnapshot pins the pre-snapshot hint.
func TestViewBeforeFirstSnapshot(t *testing.T) {
	m := newModel(t, 120, 30)
	if out := m.View(); !strings.Contains(out, "waiting for samples") {
		t.Errorf("fresh view missing the waiting hint:\n%s", out)
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
