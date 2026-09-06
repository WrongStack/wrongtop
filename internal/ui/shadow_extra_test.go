package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// Ragged box lines are padded to the widest line first so the shadow
// edge stays straight.
func TestShadowPadsRaggedLines(t *testing.T) {
	out := Shadow("ab\nabcd", lipgloss.NewStyle())
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("shadowed lines = %d, want 3", len(lines))
	}
	for i, l := range lines {
		if got := lipgloss.Width(l); got != 6 {
			t.Errorf("line %d width = %d, want 6", i, got)
		}
	}
	if got := strings.Count(stripANSIFrame(lines[2]), "░"); got != 6 {
		t.Errorf("shadow row = %q, want 6 shade cells", lines[2])
	}
	if !strings.HasPrefix(stripANSIFrame(lines[0]), "ab  ") {
		t.Errorf("short line should be padded before the shadow: %q", lines[0])
	}
}
