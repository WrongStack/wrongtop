package canvas

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestDotBitsCoverFullBlock(t *testing.T) {
	var bits byte
	for r := range 4 {
		for c := range 2 {
			bits |= dotBits[r][c]
		}
	}
	if bits != 0xff {
		t.Fatalf("all dots should cover 0xff, got %#x", bits)
	}
}

func TestGraphFullFill(t *testing.T) {
	g := New(2, 1, nil)
	for range 4 {
		g.Push(1)
	}
	view := g.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d", len(lines))
	}
	if !strings.Contains(stripANSI(view), "\u28ff\u28ff") {
		t.Fatalf("full values should render full braille blocks, got %q", stripANSI(view))
	}
}

func TestGraphHalfFillIsBottomAnchored(t *testing.T) {
	g := New(1, 1, nil)
	g.Push(0.5) // one sample per dot column (2 columns per cell)
	g.Push(0.5)
	plain := stripANSI(g.View())
	r := []rune(plain)[0] - 0x2800
	// bottom two dots of both columns on: (2,L)=0x04 (3,L)=0x40 (2,R)=0x20 (3,R)=0x80
	want := byte(0x04 | 0x40 | 0x20 | 0x80)
	if r != rune(want) {
		t.Fatalf("half fill: want bits %#x, got %#x", want, r)
	}
}

func TestGraphRingWrapKeepsNewest(t *testing.T) {
	g := New(2, 1, nil) // 4 samples
	for i := range 6 {
		g.Push(float64(i)) // 2,3,4,5 should survive
	}
	plain := stripANSI(g.View())
	// Not directly inspectable; use tail through resize instead.
	g.Resize(2, 1)
	if got := len(g.tail(g.size)); got != 4 {
		t.Fatalf("want 4 samples kept, got %d", got)
	}
	last := g.tail(g.size)[3]
	if last != 5 {
		t.Fatalf("newest sample should be 5, got %v", last)
	}
	_ = plain
}

func TestGraphResizeKeepsTail(t *testing.T) {
	g := New(4, 2, nil) // 16 samples
	for i := range 16 {
		g.Push(float64(i))
	}
	g.Resize(2, 1) // cap 4
	got := g.tail(g.size)
	want := []float64{12, 13, 14, 15}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sample %d: want %v, got %v", i, want[i], got[i])
		}
	}
}

func TestBarRounding(t *testing.T) {
	plain := func(s string) string { return stripANSI(s) }
	if got := len([]rune(plain(Bar(10, 0.5, plainStyle(), plainStyle())))); got != 10 {
		t.Fatalf("bar width: want 10, got %d", got)
	}
	full := plain(Bar(5, 1, plainStyle(), plainStyle()))
	if full != "█████" {
		t.Fatalf("full bar: got %q", full)
	}
	empty := plain(Bar(5, 0, plainStyle(), plainStyle()))
	if empty != "░░░░░" {
		t.Fatalf("empty bar: got %q", empty)
	}
}

func plainStyle() lipgloss.Style { return lipgloss.NewStyle() }

func stripANSI(s string) string {
	var sb strings.Builder
	inEsc := false
	for _, r := range s {
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}
