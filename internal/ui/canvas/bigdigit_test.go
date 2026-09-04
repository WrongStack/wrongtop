package canvas

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestBigNumberShape(t *testing.T) {
	lines := BigNumber(45, Ramp{"#00ff00"})
	if len(lines) != BigNumberHeight {
		t.Fatalf("BigNumber rows = %d, want %d", len(lines), BigNumberHeight)
	}
	if got := lipgloss.Width(lines[0]); got != BigNumberWidth(2) {
		t.Fatalf("width = %d, want %d", got, BigNumberWidth(2))
	}
	joined := strings.Join(lines, "\n")
	if strings.Count(joined, "█") < 12 {
		t.Errorf("too few blocks for two digits: %q", joined)
	}
	// every row must share the same visible width or the hero shifts
	for i, l := range lines {
		if lipgloss.Width(l) != BigNumberWidth(2) {
			t.Errorf("row %d width = %d, want %d", i, lipgloss.Width(l), BigNumberWidth(2))
		}
	}
}

func TestBigNumberDigits(t *testing.T) {
	// '1' renders as a vertical bar in the middle column
	lines := BigNumber(1, Ramp{"#ffffff"})
	if !strings.Contains(lines[0], " █ ") || !strings.Contains(lines[3], " █ ") {
		t.Errorf("digit 1 shape unexpected: %q", strings.Join(lines, "|"))
	}
	// '.' sits on the bottom row only
	g := bigDigits['.']
	for row := 0; row < 4; row++ {
		if g[row] != "   " {
			t.Errorf("dot row %d not blank: %q", row, g[row])
		}
	}
	if g[4] != " █ " {
		t.Errorf("dot baseline wrong: %q", g[4])
	}
}

func TestBigNumberClamps(t *testing.T) {
	if got := lipgloss.Width(BigNumber(1000, Ramp{"#ffffff"})[0]); got != BigNumberWidth(3) {
		t.Errorf("1000 should clamp to 3 digits, width %d", got)
	}
	if got := lipgloss.Width(BigNumber(-5, Ramp{"#ffffff"})[0]); got != BigNumberWidth(1) {
		t.Errorf("negative should clamp to 0, width %d", got)
	}
}
