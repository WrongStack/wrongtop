package app

import (
	"math"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
)

// A tab can outgrow its budget — a 3-row terminal pane, or a snapshot
// with extreme wire-delivered values — and the overflow used to push
// the status bar off the bottom row (a 3x3 terminal rendered a 222-line
// frame; a hostile snapshot 3124 lines) while over-wide rows soft-wrapped.
// Height 2 is pinned too: the empty content section used to emit a blank
// middle line, pushing the status bar off a 2-row terminal.
func TestFrameFitsTerminal(t *testing.T) {
	m := New(config.Default(), "", "test")
	snap := collector.Snapshot{Time: time.Now()}
	snap.CPU.Percent = 1e300
	snap.Mem.Percent = 1e300
	snap.Procs = []collector.Proc{
		{PID: 1, Name: strings.Repeat("p", 300), RSS: math.MaxUint64, CPU: 1e308},
	}
	for _, wh := range [][2]int{{80, 24}, {80, 30}, {40, 12}, {20, 10}, {10, 8}, {3, 3}, {80, 2}, {10, 2}, {200, 2}} {
		w, h := wh[0], wh[1]
		m.Update(tea.WindowSizeMsg{Width: w, Height: h})
		m.Update(collector.SnapshotMsg{Snap: snap})
		lines := strings.Split(m.View().Content, "\n")
		if len(lines) != h {
			t.Errorf("%dx%d: frame is %d lines, want exactly %d (tab bar + content + status bar)", w, h, len(lines), h)
		}
		for i, l := range lines {
			if lw := lipgloss.Width(l); lw > w {
				t.Errorf("%dx%d: line %d is %d cells wide, want <= %d", w, h, i, lw, w)
			}
		}
	}
}
