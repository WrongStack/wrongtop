package canvas

import (
	"strconv"
	"strings"
)

// bigDigits is a 5-row, 3-column block font — tall enough to read at a
// glance, unlike cramped 4-row attempts.
var bigDigits = map[rune][5]string{
	'0': {"███", "█ █", "█ █", "█ █", "███"},
	'1': {" █ ", " █ ", " █ ", " █ ", " █ "},
	'2': {"███", "  █", "███", "█  ", "███"},
	'3': {"███", "  █", "███", "  █", "███"},
	'4': {"█ █", "█ █", "███", "  █", "  █"},
	'5': {"███", "█  ", "███", "  █", "███"},
	'6': {"███", "█  ", "███", "█ █", "███"},
	'7': {"███", "  █", "  █", "  █", "  █"},
	'8': {"███", "█ █", "███", "█ █", "███"},
	'9': {"███", "█ █", "███", "  █", "███"},
	'.': {"   ", "   ", "   ", "   ", " █ "},
	'%': {"█ █", "  █", " █ ", "█  ", " █ "},
	' ': {"   ", "   ", "   ", "   ", "   "},
}

// BigNumberHeight is the row count of every BigNumber render.
const BigNumberHeight = 5

// BigNumberWidth reports the cell width BigNumber needs for n glyphs
// (3 columns each plus a 1-column gap between glyphs).
func BigNumberWidth(n int) int {
	if n < 1 {
		return 0
	}
	return n*4 - 1
}

// BigNumber renders v as a 5-line block-digit banner colored along the
// ramp at value/100 — the btop-style hero readout for headline metrics.
func BigNumber(v int, ramp Ramp) []string {
	v = min(max(v, 0), 999)
	s := strconv.Itoa(v)
	style := ramp.StyleAt(float64(v) / 100).Bold(true)

	lines := make([]string, BigNumberHeight)
	for row := 0; row < BigNumberHeight; row++ {
		var sb strings.Builder
		for i, ch := range s {
			if i > 0 {
				sb.WriteByte(' ')
			}
			glyph, ok := bigDigits[ch]
			if !ok {
				glyph = bigDigits[' ']
			}
			sb.WriteString(style.Render(glyph[row]))
		}
		lines[row] = sb.String()
	}
	return lines
}
