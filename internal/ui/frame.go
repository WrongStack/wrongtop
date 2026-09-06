package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Panel is one titled region of a Frame. Rectangles are inclusive
// border coordinates: a panel's right border column may be the next
// panel's left border column, in which case the two share that divider
// line — the btop-style connected look.
type Panel struct {
	X, Y, W, H int
	Title      string
	TitleStyle lipgloss.Style
	// Right is a pre-styled live value (e.g. "47.2% · 45°C") spliced
	// right-aligned into the top border line — the btop panel readout.
	Right string
	// Border colors this panel's border cells individually; nil falls
	// back to the Frame-wide border style.
	Border *lipgloss.Style
	Lines  []string // content lines, exactly W-2 cells wide each
}

// Frame renders panels inside one connected border grid. Adjacent panel
// borders coincide, junctions resolve into the proper box-drawing
// characters (├ ┤ ┬ ┴) and corners come from the given border set
// (rounded ╭╮╰╯ or square ┌┐└┘). Content that is smaller than its panel
// is padded with spaces; overflow is truncated. Panels with their own
// Border style color their border cells individually — where two colors
// meet, the later panel in the slice wins. The outer size is exactly the
// union of the panel rectangles.
func Frame(panels []Panel, border lipgloss.Style, corners lipgloss.Border) string {
	W, H := 0, 0
	for _, p := range panels {
		W = max(W, p.X+p.W)
		H = max(H, p.Y+p.H)
	}
	if W < 2 || H < 2 {
		return ""
	}

	// mark stores the border character for every border cell and owner
	// the panel index that claimed it, for per-panel border colors.
	mark := make([][]rune, H)
	owner := make([][]int, H)
	for i := range mark {
		mark[i] = []rune(strings.Repeat(" ", W))
		owner[i] = make([]int, W)
		for x := range owner[i] {
			owner[i][x] = -1
		}
	}
	set := func(p Panel, pi, y, x int, r rune) {
		if y >= 0 && y < H && x >= 0 && x < W {
			mark[y][x] = r
			owner[y][x] = pi
		}
	}
	for pi, p := range panels {
		for x := p.X; x < p.X+p.W; x++ {
			set(p, pi, p.Y, x, '─')
			set(p, pi, p.Y+p.H-1, x, '─')
		}
		for y := p.Y; y < p.Y+p.H; y++ {
			set(p, pi, y, p.X, '│')
			set(p, pi, y, p.X+p.W-1, '│')
		}
	}

	// resolve every marked cell into the junction character matching its
	// four-way connectivity: corners on the outside, ┼ where segments
	// cross, ├ ┤ ┬ ┴ where three meet.
	connected := func(y, x int, dy, dx int) bool {
		ny, nx := y+dy, x+dx
		if ny < 0 || ny >= H || nx < 0 || nx >= W {
			return false
		}
		return mark[ny][nx] == '─' || mark[ny][nx] == '│'
	}
	// corner runes of the configured border set (rounded or square)
	corner := func(s string) rune {
		for _, r := range s {
			return r
		}
		return '?'
	}
	tl, tr := corner(corners.TopLeft), corner(corners.TopRight)
	bl, br := corner(corners.BottomLeft), corner(corners.BottomRight)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			if mark[y][x] == ' ' {
				continue
			}
			n, s := connected(y, x, -1, 0), connected(y, x, 1, 0)
			e, w := connected(y, x, 0, 1), connected(y, x, 0, -1)
			switch {
			case n && s && e && w:
				mark[y][x] = '┼'
			case n && s && e:
				mark[y][x] = '├'
			case n && s && w:
				mark[y][x] = '┤'
			case e && w && n:
				mark[y][x] = '┴'
			case e && w && s:
				mark[y][x] = '┬'
			case s && e:
				mark[y][x] = tl
			case s && w:
				mark[y][x] = tr
			case n && e:
				mark[y][x] = bl
			case n && w:
				mark[y][x] = br
			case n || s:
				mark[y][x] = '│'
			default:
				mark[y][x] = '─'
			}
		}
	}

	// content spans per output row: (startCol, endColExclusive, text)
	type span struct {
		x0, x1 int
		text   string
	}
	rows := make([][]span, H)
	for _, p := range panels {
		for i, line := range p.Lines {
			y := p.Y + 1 + i
			if y >= p.Y+p.H-1 {
				break // never draw over the bottom border
			}
			// every span is exactly the content width: truncated when
			// long (Width alone would wrap it into extra rows) and
			// padded when short (so rows align)
			w := p.W - 2
			if lipgloss.Width(line) > w {
				line = ansi.Truncate(line, w, "")
			}
			rows[y] = append(rows[y], span{
				x0:   p.X + 1,
				x1:   p.X + p.W - 1,
				text: lipgloss.NewStyle().Width(w).Render(line),
			})
		}
	}

	// per-panel border colors: cells claimed by a panel with its own
	// Border style render in that color, the rest in the shared one.
	// Same-owner runs are batched into one Render call.
	styleFor := func(y, x int) lipgloss.Style {
		if pi := owner[y][x]; pi >= 0 && panels[pi].Border != nil {
			return *panels[pi].Border
		}
		return border
	}
	renderRun := func(y, x0, x1 int) string {
		var sb strings.Builder
		for cx := x0; cx < x1; cx++ {
			sb.WriteRune(mark[y][cx])
		}
		return styleFor(y, x0).Render(sb.String())
	}

	out := make([]string, H)
	for y := 0; y < H; y++ {
		var b strings.Builder
		x := 0
		spans := rows[y]
		for _, sp := range spans {
			for x < sp.x0 { // border cells left of the span
				run := x + 1
				for run < sp.x0 && owner[y][run] == owner[y][x] {
					run++
				}
				b.WriteString(renderRun(y, x, run))
				x = run
			}
			b.WriteString(sp.text)
			x = sp.x1
		}
		for x < W { // border cells after the last span
			run := x + 1
			for run < W && owner[y][run] == owner[y][x] {
				run++
			}
			b.WriteString(renderRun(y, x, run))
			x = run
		}
		out[y] = b.String()
	}

	// chrome goes on last so it sits cleanly in the top border: the
	// title leads from the panel's left edge and the live value trails
	// flush against the right border, each shifted off any junction
	// column the divider crossings put in the way.
	junction := func(y, x int) bool {
		switch mark[y][x] {
		case '┬', '┼', '├', '┤', '┴':
			return true
		}
		return false
	}
	junctionFree := func(y, start, w int) bool {
		for x := start; x < start+w; x++ {
			if junction(y, x) {
				return false
			}
		}
		return true
	}
	for _, p := range panels {
		lo := p.X + 1       // first content column
		hi := p.X + p.W - 2 // last content column
		used := lo          // right chrome must clear the title
		if label := " " + p.Title + " "; label != "  " {
			lw := lipgloss.Width(label)
			for start := lo; start+lw-1 <= hi; start++ {
				if junctionFree(p.Y, start, lw) {
					out[p.Y] = spliceStyled(out[p.Y], start, label, p.TitleStyle)
					used = start + lw
					break
				}
			}
		}
		if p.Right == "" {
			continue
		}
		rw := lipgloss.Width(p.Right)
		start := hi - rw + 1 // end flush against the right border
		for ; start >= used && !junctionFree(p.Y, start, rw); start-- {
		}
		if start >= used {
			out[p.Y] = spliceStyled(out[p.Y], start, p.Right, lipgloss.NewStyle())
		}
	}
	return strings.Join(out, "\n")
}

// spliceStyled replaces width cells starting at start with the given
// already-styled text, preserving the ANSI-wrapped remainder of the
// line. ansi.Cut cuts by printable cells while keeping escape sequences
// intact.
func spliceStyled(line string, start int, text string, style lipgloss.Style) string {
	prefix := ansi.Cut(line, 0, start)
	rest := ansi.Cut(line, start+lipgloss.Width(text), lipgloss.Width(line)+1)
	return prefix + style.Render(text) + rest
}
