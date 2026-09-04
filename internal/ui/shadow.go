package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Shadow adds a btop-style drop shadow under a rendered box: two dim
// shade columns on the right and a dim shade row underneath. The box
// lines may carry ANSI styling; they are padded to a common width
// first so the shadow edge stays straight.
func Shadow(box string, dim lipgloss.Style) string {
	lines := strings.Split(box, "\n")
	w := 0
	for _, l := range lines {
		w = max(w, lipgloss.Width(l))
	}
	edge := dim.Render(strings.Repeat("░", 2))
	for i, l := range lines {
		if pad := w - lipgloss.Width(l); pad > 0 {
			l += strings.Repeat(" ", pad)
		}
		lines[i] = l + edge
	}
	lines = append(lines, dim.Render(strings.Repeat("░", w+2)))
	return strings.Join(lines, "\n")
}
