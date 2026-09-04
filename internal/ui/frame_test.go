package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func framePanels() []Panel {
	// the bottom band's top border (Y=7) IS the top band's bottom border:
	// panels share divider lines, btop-style
	return []Panel{
		{X: 0, Y: 0, W: 20, H: 8, Title: "LEFT", Lines: []string{"alpha", "beta"}},
		{X: 20, Y: 0, W: 20, H: 8, Title: "RIGHT", Lines: []string{"gamma"}},
		{X: 0, Y: 7, W: 40, H: 6, Title: "BOTTOM", Lines: []string{"delta"}},
	}
}

func noStyle(s string) lipgloss.Style { return lipgloss.NewStyle() }

func TestFrameSizeAndBorder(t *testing.T) {
	out := Frame(framePanels(), noStyle(""))
	lines := strings.Split(out, "\n")
	if len(lines) != 13 {
		t.Fatalf("frame height = %d, want 13 (shared divider)", len(lines))
	}
	if got := lipgloss.Width(out); got != 40 {
		t.Fatalf("frame width = %d, want 40", got)
	}

	// rounded outer corners
	if !strings.Contains(lines[0], "╭") || !strings.Contains(lines[0], "╮") {
		t.Errorf("top corners missing: %q", lines[0])
	}
	if !strings.Contains(lines[12], "╰") || !strings.Contains(lines[12], "╯") {
		t.Errorf("bottom corners missing: %q", lines[12])
	}

	// junctions: ┬ where the divider meets the top edge, ┴ at the
	// four-way meeting (the divider stops at the bottom band's top
	// border), ├/┤ on the frame edges at the shared divider
	if !strings.Contains(lines[0], "┬") {
		t.Errorf("top T-junction missing: %q", lines[0])
	}
	if !strings.Contains(lines[7], "┴") {
		t.Errorf("divider junction missing at the shared border: %q", lines[7])
	}
	if !strings.Contains(lines[7], "├") || !strings.Contains(lines[7], "┤") {
		t.Errorf("edge junctions missing: %q", lines[7])
	}
}

func TestFrameContentPlacement(t *testing.T) {
	out := Frame(framePanels(), noStyle(""))
	for _, want := range []string{"alpha", "beta", "gamma", "delta"} {
		if !strings.Contains(out, want) {
			t.Errorf("frame missing content %q", want)
		}
	}
	for _, want := range []string{"LEFT", "RIGHT", "BOTTOM"} {
		if !strings.Contains(out, want) {
			t.Errorf("frame missing title %q", want)
		}
	}
}

func TestFrameOverflowTruncates(t *testing.T) {
	panels := []Panel{
		{X: 0, Y: 0, W: 10, H: 4, Title: "P", Lines: []string{
			"0123456789012345", // wider than the 8-cell content area
			"line2",
			"line3", // beyond the 2-row content area
		}},
	}
	out := Frame(panels, noStyle(""))
	lines := strings.Split(out, "\n")
	if len(lines) != 4 {
		t.Fatalf("height = %d, want 4", len(lines))
	}
	if got := lipgloss.Width(lines[1]); got != 10 {
		t.Errorf("content line width = %d, want 10 (content must not stretch the frame)", got)
	}
	if strings.Contains(out, "line3") {
		t.Error("content past the panel bottom leaked")
	}
	if strings.Contains(out, "012345678901") {
		t.Error("content wider than the panel leaked")
	}
}

func TestFrameSinglePanel(t *testing.T) {
	out := Frame([]Panel{{X: 0, Y: 0, W: 12, H: 5, Title: "ONLY", Lines: []string{"x"}}}, noStyle(""))
	lines := strings.Split(out, "\n")
	if len(lines) != 5 || lipgloss.Width(out) != 12 {
		t.Fatalf("size = %dx%d, want 5x12", len(lines), lipgloss.Width(out))
	}
	if strings.Contains(lines[0], "┬") || strings.Contains(lines[0], "┼") {
		t.Error("single panel should have plain corners")
	}
}
