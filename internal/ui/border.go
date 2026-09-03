package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Box draws content inside a border with the title embedded in the top
// border line, btop-style. charStyle colors the border runes themselves
// (a color-only style — passing the bordered style here would recursively
// box the border characters).
func Box(border lipgloss.Border, boxStyle, charStyle, titleStyle lipgloss.Style, title, content string) string {
	w := lipgloss.Width(content)
	t := titleStyle.Render(" " + title + " ")
	tw := lipgloss.Width(t)

	inner := max(w, tw) // pad so the title always fits
	body := boxStyle.Render(
		lipgloss.NewStyle().Width(inner).Render(content),
	)
	lines := strings.Split(body, "\n")

	top := charStyle.Render(border.TopLeft) + t
	if fill := inner - tw; fill > 0 {
		top += charStyle.Render(strings.Repeat(border.Top, fill))
	}
	top += charStyle.Render(border.TopRight)
	lines[0] = top

	return strings.Join(lines, "\n")
}
