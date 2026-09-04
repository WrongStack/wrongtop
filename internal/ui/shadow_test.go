package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestShadow(t *testing.T) {
	box := "┌──┐\n│ab│\n└──┘"
	out := Shadow(box, lipgloss.NewStyle())
	lines := strings.Split(out, "\n")
	if len(lines) != 4 {
		t.Fatalf("shadowed lines = %d, want 4 (box + shadow row)", len(lines))
	}
	for i, l := range lines[:3] {
		if got := lipgloss.Width(l); got != 6 { // 4 box + 2 shadow columns
			t.Errorf("line %d width = %d, want 6", i, got)
		}
	}
	if got := lipgloss.Width(lines[3]); got != 6 {
		t.Errorf("shadow row width = %d, want 6", got)
	}
	if !strings.Contains(lines[3], "░") {
		t.Error("shadow row missing shade character")
	}
}
