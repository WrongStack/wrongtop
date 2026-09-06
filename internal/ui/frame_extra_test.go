package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// Frames smaller than two border cells cannot pay for themselves.
func TestFrameTooSmallIsEmpty(t *testing.T) {
	for _, panels := range [][]Panel{
		nil,
		{{X: 0, Y: 0, W: 1, H: 3}},
		{{X: 0, Y: 0, W: 4, H: 1}},
	} {
		if got := Frame(panels, noStyle(""), lipgloss.RoundedBorder()); got != "" {
			t.Errorf("Frame(%d panels) = %q, want \"\"", len(panels), got)
		}
	}
}

// Four quadrants sharing their divider columns and row resolve the
// crossing into ┼ (adjacent-but-not-shared borders would not).
func TestFrameCrossJunction(t *testing.T) {
	panels := []Panel{
		{X: 0, Y: 0, W: 21, H: 8, Lines: []string{"tl"}},  // right border x=20
		{X: 20, Y: 0, W: 20, H: 8, Lines: []string{"tr"}}, // left border x=20: shared
		{X: 0, Y: 7, W: 21, H: 6, Lines: []string{"bl"}},
		{X: 20, Y: 7, W: 20, H: 6, Lines: []string{"br"}},
	}
	out := Frame(panels, noStyle(""), lipgloss.RoundedBorder())
	lines := strings.Split(out, "\n")
	if len(lines) != 13 || lipgloss.Width(out) != 40 {
		t.Fatalf("frame = %d lines x %d cells, want 13x40", len(lines), lipgloss.Width(out))
	}
	if !strings.Contains(lines[7], "┼") {
		t.Errorf("four-way junction missing: %q", lines[7])
	}
	if !strings.Contains(lines[0], "┬") || !strings.Contains(lines[12], "┴") {
		t.Errorf("T junctions missing on top/bottom: %q / %q", lines[0], lines[12])
	}
	for _, want := range []string{"╭", "╮", "╰", "╯", "├", "┤"} {
		if !strings.Contains(out, want) {
			t.Errorf("frame missing %q", want)
		}
	}
	if !strings.Contains(out, "tl") || !strings.Contains(out, "br") {
		t.Errorf("quadrant content missing: %q", out)
	}
}

// Panels may color their own border cells; the shared style covers the rest.
func TestFramePerPanelBorder(t *testing.T) {
	own := lipgloss.NewStyle()
	panels := []Panel{
		{X: 0, Y: 0, W: 10, H: 4, Title: "A", Border: &own, Lines: []string{"a"}},
		{X: 10, Y: 0, W: 10, H: 4, Title: "B", Lines: []string{"b"}},
	}
	out := Frame(panels, noStyle(""), lipgloss.NormalBorder())
	if got := lipgloss.Width(out); got != 20 {
		t.Fatalf("frame width = %d, want 20", got)
	}
	for _, want := range []string{"a", "b", "A", "B"} {
		if !strings.Contains(out, want) {
			t.Errorf("frame missing %q", want)
		}
	}
}

// A row with two separated panels batches the gap cells into one render
// run instead of one call per cell.
func TestFrameGapBetweenPanels(t *testing.T) {
	panels := []Panel{
		{X: 0, Y: 0, W: 10, H: 4, Lines: []string{"aaaa"}},
		{X: 12, Y: 0, W: 10, H: 4, Lines: []string{"bbbb"}},
	}
	out := Frame(panels, noStyle(""), lipgloss.RoundedBorder())
	lines := strings.Split(out, "\n")
	if len(lines) != 4 || lipgloss.Width(out) != 22 {
		t.Fatalf("frame = %d lines x %d cells, want 4x22", len(lines), lipgloss.Width(out))
	}
	want := "│aaaa    │  │bbbb    │"
	if got := stripANSIFrame(lines[1]); got != want {
		t.Errorf("content row = %q, want %q", got, want)
	}
}

// A title that cannot clear the shared-divider junction is dropped
// rather than drawn over it.
func TestFrameTitleDroppedOnJunction(t *testing.T) {
	panels := []Panel{
		{X: 0, Y: 0, W: 20, H: 8, Lines: []string{"a"}},
		{X: 20, Y: 0, W: 20, H: 8, Lines: []string{"b"}},
		{X: 0, Y: 7, W: 40, H: 6, Title: "A-MUCH-LONGER-BOTTOM-TITLE", Lines: []string{"c"}},
	}
	out := Frame(panels, noStyle(""), lipgloss.RoundedBorder())
	lines := strings.Split(out, "\n")
	if got := stripANSIFrame(lines[7]); strings.Contains(got, "MUCH-LONGER") {
		t.Errorf("title overlapping the ┴ junction should be dropped: %q", got)
	}
	if !strings.Contains(stripANSIFrame(out), "c") {
		t.Errorf("panel content must survive: %q", out)
	}
}

// A right readout that lands on a junction walks left until the range is
// clear, then splices in without touching the junction columns.
func TestFrameReadoutWalksPastJunction(t *testing.T) {
	panels := []Panel{
		{X: 0, Y: 0, W: 21, H: 6, Lines: []string{"a"}},  // right border at x=20
		{X: 20, Y: 0, W: 10, H: 6, Lines: []string{"b"}}, // shares the x=20 divider
		// its top border crosses the divider: a ┴ junction at x=20, a ┤ at x=29
		{X: 0, Y: 5, W: 30, H: 6, Title: "D", Right: "R0123456789B", Lines: []string{"c"}},
	}
	out := Frame(panels, noStyle(""), lipgloss.RoundedBorder())
	lines := strings.Split(out, "\n")
	plain := stripANSIFrame(lines[5])
	// the readout walked left off the x=20 junction and ends flush against it
	if !strings.Contains(plain, "R0123456789B┴") {
		t.Errorf("readout should end just left of the junction: %q", plain)
	}
	if !strings.Contains(plain, "┤") || !strings.Contains(plain, "├") {
		t.Errorf("frame edge junctions must survive the splice: %q", plain)
	}
	if !strings.Contains(plain, " D ") {
		t.Errorf("title should stay in place: %q", plain)
	}
}

// Border sets with empty corner strings degrade to a placeholder rune.
func TestFrameBorderWithoutCorners(t *testing.T) {
	bare := lipgloss.Border{Top: "─", Bottom: "─", Left: "│", Right: "│"}
	out := Frame([]Panel{{X: 0, Y: 0, W: 6, H: 3, Lines: []string{"x"}}},
		noStyle(""), bare)
	if got := strings.Count(out, "?"); got != 4 {
		t.Errorf("empty corners should render four placeholders, got %d in %q", got, out)
	}
}
