package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// BorderFor resolves the configured panel corner style: "square" gives
// btop's sharp corners, "thick"/"double" the heavy ruled sets, anything
// else the rounded default.
func BorderFor(name string) lipgloss.Border {
	switch name {
	case "square":
		return lipgloss.NormalBorder()
	case "thick":
		return lipgloss.ThickBorder()
	case "double":
		return lipgloss.DoubleBorder()
	default:
		return lipgloss.RoundedBorder()
	}
}

// Box draws content inside a border with the title embedded at the left
// of the top border line and an optional pre-styled live value flush
// right — the same chrome the grid Frame gives its panels. charStyle
// colors the border runes themselves (a color-only style — passing the
// bordered style here would recursively box the border characters).
func Box(border lipgloss.Border, boxStyle, charStyle, titleStyle lipgloss.Style, title, right, content string) string {
	w := lipgloss.Width(content)
	t := titleStyle.Render(" " + title + " ")
	tw := lipgloss.Width(t)
	rw := lipgloss.Width(right)

	inner := max(w, tw+rw) // pad so the chrome always fits
	body := boxStyle.Render(
		lipgloss.NewStyle().Width(inner).Render(content),
	)
	lines := strings.Split(body, "\n")

	top := charStyle.Render(border.TopLeft) + t
	if fill := inner - tw - rw; fill > 0 {
		top += charStyle.Render(strings.Repeat(border.Top, fill))
	}
	top += right + charStyle.Render(border.TopRight)
	lines[0] = top

	return strings.Join(lines, "\n")
}
