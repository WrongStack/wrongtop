package canvas

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// SetMax fixes the scale top; non-positive values must not clobber it.
func TestGraphSetMax(t *testing.T) {
	g := New(2, 1, nil)
	g.SetMax(0)
	if g.max != 1 {
		t.Errorf("SetMax(0) should keep the default max, got %v", g.max)
	}
	g.SetMax(-1)
	if g.max != 1 {
		t.Errorf("SetMax(-1) should keep the default max, got %v", g.max)
	}
	g.SetMax(5)
	if g.max != 5 {
		t.Errorf("SetMax(5) should set the max, got %v", g.max)
	}
	// values above the fixed max clamp to it in View
	g.Push(10)
	g.Push(10)
	if plain := stripANSI(g.View()); !strings.Contains(plain, "\u28ff") {
		t.Errorf("values above max should render full braille cells, got %q", plain)
	}
}

// Negative samples clamp to the floor instead of corrupting the fill.
func TestGraphNegativeSampleClamps(t *testing.T) {
	g := New(2, 1, nil)
	g.Push(-3)
	g.Push(-3)
	if plain := stripANSI(g.View()); strings.Contains(plain, "\u28ff") {
		t.Errorf("negative samples should render an empty cell, got %q", plain)
	}
}

func TestGraphSetRamp(t *testing.T) {
	g := New(2, 1, testRamp.Colors())
	g.Push(1)
	g.Push(1)
	before := stripANSI(g.View())

	g.SetRamp(Ramp{"#ff0000"}.Colors()) // live theme swap
	if after := stripANSI(g.View()); after != before {
		t.Errorf("a ramp swap must not change the shape:\n%q\n%q", before, after)
	}

	g.SetRamp(nil) // empty ramps degrade to the unstyled fallback
	if plain := stripANSI(g.View()); !strings.Contains(plain, "\u28ff") {
		t.Errorf("graph should still render after SetRamp(nil), got %q", plain)
	}
}

func TestGraphResizeClampsAndNoOps(t *testing.T) {
	g := New(3, 2, nil)
	for i := range 10 {
		g.Push(float64(i))
	}
	g.Resize(3, 2) // same shape: early return, samples stay put
	if got := g.tail(g.size); len(got) != 6 || got[5] != 9 {
		t.Fatalf("no-op resize should keep the ring's newest samples, got %v", got)
	}
	g.Resize(0, 0) // nonsense sizes clamp to the 1x1 minimum
	if g.width != 1 || g.height != 1 {
		t.Errorf("resize(0,0) = %dx%d, want 1x1", g.width, g.height)
	}
	g.Push(42) // the clamped ring must stay usable
	if got := g.tail(1); len(got) != 1 || got[0] != 42 {
		t.Errorf("push after clamped resize: tail = %v, want [42]", got)
	}
}

// styleFor clamps its index into the table for out-of-range inputs.
func TestGraphStyleForClamps(t *testing.T) {
	g := New(2, 1, testRamp.Colors())
	if got := g.styleFor(-0.5).Render("x"); got != g.styles[0].Render("x") {
		t.Error("styleFor below the range should clamp to the first style")
	}
	if got := g.styleFor(1.5).Render("x"); got != g.styles[len(g.styles)-1].Render("x") {
		t.Error("styleFor above the range should clamp to the last style")
	}
}

func TestOverlayLabel(t *testing.T) {
	if got := OverlayLabel("0123456789\nab", ""); got != "0123456789\nab" {
		t.Errorf("empty label should return the block unchanged, got %q", got)
	}
	if got := OverlayLabel("abc", "LONG"); got != "abc" {
		t.Errorf("label that leaves no strip should return the block unchanged, got %q", got)
	}
	// tw-lw == 4 is the boundary: still enough room
	if got := OverlayLabel("012345", "XY"); got != "0123XY" {
		t.Errorf("boundary overlay = %q, want %q", got, "0123XY")
	}
	if got := OverlayLabel("0123456789\nab", "PK"); got != "01234567PK\nab" {
		t.Errorf("multiline overlay = %q, want %q", got, "01234567PK\nab")
	}
}

func TestBarEdges(t *testing.T) {
	st := plainStyle()
	if got := Bar(0, 0.5, st, st); got != "" {
		t.Errorf("zero width Bar = %q, want \"\"", got)
	}
	if got := stripANSI(Bar(10, 0.25, st, st)); got != "██▌░░░░░░░" {
		t.Errorf("eighth-edge bar = %q, want %q", got, "██▌░░░░░░░")
	}
}

func TestBigNumberWidthEdges(t *testing.T) {
	cases := map[int]int{
		0:  0,
		-2: 0,
		1:  3,
		4:  15,
	}
	for n, want := range cases {
		if got := BigNumberWidth(n); got != want {
			t.Errorf("BigNumberWidth(%d) = %d, want %d", n, got, want)
		}
	}
}

// Every digit glyph renders with the shared ramp style; widths stay
// uniform across all ten digits.
func TestBigNumberAllDigits(t *testing.T) {
	for d := rune('0'); d <= '9'; d++ {
		glyph, ok := bigDigits[d]
		if !ok {
			t.Fatalf("digit %c missing from bigDigits", d)
		}
		for row, cells := range glyph {
			if len([]rune(cells)) != 3 {
				t.Errorf("digit %c row %d = %q, want 3 cells", d, row, cells)
			}
		}
	}
	for d := 0; d <= 9; d++ {
		lines := BigNumber(d, testRamp)
		if len(lines) != BigNumberHeight {
			t.Fatalf("BigNumber(%d) rows = %d, want %d", d, len(lines), BigNumberHeight)
		}
		for i, l := range lines {
			if got := lipgloss.Width(l); got != 3 {
				t.Errorf("BigNumber(%d) row %d width = %d, want 3", d, i, got)
			}
		}
	}
}
