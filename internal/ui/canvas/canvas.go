// Package canvas draws time-series graphs with braille characters,
// btop-style: 4 dot rows and 2 dot columns packed per terminal cell.
package canvas

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// dotBits maps a dot position inside a braille cell (row 0-3 top-down,
// col 0-1 left-right) to its bit in the braille code point.
//
//	(1) (4)      row 0
//	(2) (5)      row 1
//	(3) (6)      row 2
//	(7) (8)      row 3
var dotBits = [4][2]byte{
	{0x01, 0x08},
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80},
}

// brailleBase is the first code point of the braille patterns block.
const brailleBase = 0x2800

// Graph is a scrolling line graph backed by a ring buffer. Values are
// fractions in [0,1] (or [0,Max] when Max is set) and render as an area
// filled from the bottom. Use New; the zero value is not usable.
type Graph struct {
	ramp   []color.Color
	styles []lipgloss.Style

	data   []float64
	head   int // next write index
	size   int // valid samples
	width  int // cells
	height int // cells
	max    float64
}

// New returns a graph of the given cell size. The ramp colors cells by
// the value of their column, from low to high.
func New(width, height int, ramp []color.Color) *Graph {
	g := &Graph{ramp: ramp, max: 1}
	g.resize(width, height)
	g.buildStyles()
	return g
}

// SetMax fixes the scale top; values are expected within [0, max].
func (g *Graph) SetMax(max float64) {
	if max > 0 {
		g.max = max
	}
}

// Resize keeps the newest samples that fit the new capacity.
func (g *Graph) Resize(width, height int) {
	if width == g.width && height == g.height {
		return
	}
	g.resize(width, height)
}

func (g *Graph) resize(width, height int) {
	width, height = max(1, width), max(1, height)
	cap := width * 2
	next := make([]float64, cap)
	n := copy(next, g.tail(min(g.size, cap)))
	g.data, g.head, g.size = next, n%cap, n
	g.width, g.height = width, height
}

// tail returns the newest n samples in chronological order.
func (g *Graph) tail(n int) []float64 {
	if n <= 0 || g.data == nil {
		return nil
	}
	start := (g.head - n + len(g.data)) % len(g.data)
	if start+n <= len(g.data) {
		return g.data[start : start+n]
	}
	out := make([]float64, 0, n)
	out = append(out, g.data[start:]...)
	out = append(out, g.data[:n-len(g.data)+start]...)
	return out
}

func (g *Graph) buildStyles() {
	if len(g.ramp) == 0 {
		g.ramp = []color.Color{nil}
	}
	g.styles = make([]lipgloss.Style, len(g.ramp))
	for i, c := range g.ramp {
		g.styles[i] = lipgloss.NewStyle().Foreground(c)
	}
}

// Push appends one sample.
func (g *Graph) Push(v float64) {
	g.data[g.head] = v
	g.head = (g.head + 1) % len(g.data)
	if g.size < len(g.data) {
		g.size++
	}
}

// View renders the graph as `height` lines of `width` cells each.
func (g *Graph) View() string {
	cols := g.width * 2
	rows := g.height * 4
	samples := g.tail(g.size)

	// levels holds the filled dot height per sample column.
	levels := make([]float64, cols)
	offset := cols - len(samples)
	for i, v := range samples {
		if v < 0 {
			v = 0
		}
		if v > g.max {
			v = g.max
		}
		levels[offset+i] = v / g.max * float64(rows)
	}

	lines := make([]string, g.height)
	for cy := 0; cy < g.height; cy++ {
		var sb strings.Builder
		for cx := 0; cx < g.width; cx++ {
			var bits byte
			for c := 0; c < 2; c++ {
				lvl := levels[cx*2+c]
				for r := 0; r < 4; r++ {
					// distance of this dot row from the bottom
					if float64(rows-1-(cy*4+r)) < lvl {
						bits |= dotBits[r][c]
					}
				}
			}
			// color by the mean value of the two columns in this cell
			v := (levels[cx*2] + levels[cx*2+1]) / 2 / float64(rows)
			sb.WriteString(g.styleFor(v).Render(string(rune(brailleBase + int(bits)))))
		}
		lines[cy] = sb.String()
	}
	return strings.Join(lines, "\n")
}

func (g *Graph) styleFor(v float64) lipgloss.Style {
	i := int(v * float64(len(g.styles)-1))
	if i < 0 {
		i = 0
	}
	if i >= len(g.styles) {
		i = len(g.styles) - 1
	}
	return g.styles[i]
}

// eighthBlocks are the vertical fill runes from 1/8 to 7/8.
var eighthBlocks = []rune{'▏', '▎', '▍', '▌', '▋', '▊', '▉'}

// Bar renders a horizontal fraction bar of the given cell width using
// full blocks and eighth-block edges.
func Bar(width int, frac float64, filled, empty lipgloss.Style) string {
	if width < 1 {
		return ""
	}
	frac = min(max(frac, 0), 1)
	eighths := int(frac*float64(width)*8 + 0.5)
	full, rem := eighths/8, eighths%8

	var sb strings.Builder
	if full > width { // rounding overflow at frac==1
		full = width
		rem = 0
	}
	for range full {
		sb.WriteString(filled.Render("█"))
	}
	if rem > 0 {
		sb.WriteString(filled.Render(string(eighthBlocks[rem-1])))
	}
	for i := full + min(rem, 1); i < width; i++ {
		sb.WriteString(empty.Render("░"))
	}
	return sb.String()
}
