package canvas

import (
	"math"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// Ramp is an ordered list of hex colors ("#rrggbb") interpolated left to
// right across bars and sparklines.
type Ramp []string

// At interpolates the ramp at t ∈ [0,1] and returns a hex color.
func (r Ramp) At(t float64) string {
	t = min(max(t, 0), 1)
	if len(r) == 0 {
		return "#888888"
	}
	if len(r) == 1 {
		return r[0]
	}
	seg := t * float64(len(r)-1)
	i := int(seg)
	f := seg - float64(i)
	c1, ok1 := parseHex(r[i])
	c2, ok2 := parseHex(r[min(i+1, len(r)-1)])
	if !ok1 || !ok2 {
		return r[i]
	}
	mix := func(a, b uint8) uint8 {
		return uint8(math.Round(float64(a) + (float64(b)-float64(a))*f))
	}
	return hex(mix(c1[0], c2[0]), mix(c1[1], c2[1]), mix(c1[2], c2[2]))
}

// parseHex decodes "#rrggbb" (or "rrggbb") into RGB bytes.
func parseHex(s string) ([3]uint8, bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return [3]uint8{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return [3]uint8{}, false
	}
	return [3]uint8{uint8(v >> 16), uint8(v >> 8), uint8(v)}, true
}

func hex(r, g, b uint8) string {
	return "#" + strconv.FormatUint(uint64(r)<<16|uint64(g)<<8|uint64(b), 16)
}

// GradientBar renders a btop-style meter: the filled part interpolates
// across the ramp along the bar's length (so long bars sweep green →
// red by position, not by threshold), the remainder renders dim. The
// edge cell uses an eighth block for sub-cell precision.
func GradientBar(width int, frac float64, ramp Ramp, empty lipgloss.Style) string {
	if width < 1 {
		return ""
	}
	frac = min(max(frac, 0), 1)
	eighths := int(frac*float64(width)*8 + 0.5)
	full, rem := eighths/8, eighths%8
	if full > width { // rounding overflow at frac==1
		full, rem = width, 0
	}

	var sb strings.Builder
	for i := 0; i < full; i++ {
		t := (float64(i) + 0.5) / float64(width)
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ramp.At(t))).Render("█"))
	}
	if rem > 0 {
		t := (float64(full) + float64(rem)/8) / float64(width)
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ramp.At(t))).Render(string(eighthBlocks[rem-1])))
	}
	for i := full + min(rem, 1); i < width; i++ {
		sb.WriteString(empty.Render("░"))
	}
	return sb.String()
}

// sparkBlocks are the vertical fill runes from empty to full for
// one-line sparklines.
var sparkBlocks = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// Sparkline renders the values (each 0..1) as one row of block glyphs,
// colored per glyph by ramping on the value.
func Sparkline(values []float64, ramp Ramp) string {
	var sb strings.Builder
	for _, v := range values {
		v = min(max(v, 0), 1)
		idx := int(v*float64(len(sparkBlocks)-1) + 0.5)
		if idx >= len(sparkBlocks) {
			idx = len(sparkBlocks) - 1
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ramp.At(v))).Render(string(sparkBlocks[idx])))
	}
	return sb.String()
}

// SparklineScaled renders raw-rate samples (any magnitude) as one row of
// block glyphs, scaling the window so its tallest sample fills the full
// height — the btop activity look. All-zero windows render as blanks.
func SparklineScaled(values []float64, ramp Ramp) string {
	peak := 0.0
	for _, v := range values {
		peak = max(peak, v)
	}
	if peak <= 0 {
		return strings.Repeat(string(sparkBlocks[0]), len(values))
	}
	scaled := make([]float64, len(values))
	for i, v := range values {
		scaled[i] = v / peak
	}
	return Sparkline(scaled, ramp)
}
