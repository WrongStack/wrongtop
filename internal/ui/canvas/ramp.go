package canvas

import (
	"image/color"
	"math"
	"strconv"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
)

// Ramp is an ordered list of hex colors ("#rrggbb") interpolated left to
// right across bars and sparklines. Ramps are treated as immutable once
// built: derived styles are cached per instance.
type Ramp []string

// Colors adapts the ramp to the []color.Color shape canvas.Graph takes.
func (r Ramp) Colors() []color.Color {
	out := make([]color.Color, len(r))
	for i, h := range r {
		out[i] = lipgloss.Color(h)
	}
	return out
}

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

// styleSteps is the granularity of the per-ramp style cache; 48 steps
// are indistinguishable from per-cell interpolation at terminal sizes.
const styleSteps = 48

// styleKey identifies a ramp by its backing array — theme-owned ramps
// are stable slices, so the pointer pins the instance without hashing
// the colors on every lookup. Literal ramps simply miss the cache.
type styleKey struct {
	p *string
	n int
}

var (
	styleMu    sync.Mutex
	styleCache = map[styleKey][styleSteps]lipgloss.Style{}
)

// styles returns the cached per-step foreground styles for the ramp, so
// bar and sparkline rendering never builds a lipgloss style per cell
// per frame. The cache holds a handful of entries (one per ramp in
// play); it resets if a caller churns fresh literal ramps past the bound.
func (r Ramp) styles() [styleSteps]lipgloss.Style {
	if len(r) == 0 {
		return [styleSteps]lipgloss.Style{}
	}
	key := styleKey{p: &r[0], n: len(r)}
	styleMu.Lock()
	defer styleMu.Unlock()
	if st, ok := styleCache[key]; ok {
		return st
	}
	var st [styleSteps]lipgloss.Style
	for i := range st {
		st[i] = lipgloss.NewStyle().Foreground(lipgloss.Color(r.At(float64(i) / float64(styleSteps-1))))
	}
	if len(styleCache) >= 64 {
		styleCache = map[styleKey][styleSteps]lipgloss.Style{}
	}
	styleCache[key] = st
	return st
}

// StyleAt returns a cached foreground style for t ∈ [0,1] along the
// ramp — the allocation-free path for coloring values (table cells,
// hero digits).
func (r Ramp) StyleAt(t float64) lipgloss.Style {
	st := r.styles()
	if len(st) == 0 {
		return lipgloss.NewStyle()
	}
	i := int(min(max(t, 0), 1)*float64(styleSteps-1) + 0.5)
	return st[i]
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

// barSteps quantizes the fill fraction for the bar cache: 240 steps are
// finer than the eighth-block edge, so the memoized bar is visually
// identical to a fresh render.
const barSteps = 240

// barKey identifies a rendered bar fill: which ramps (by backing array,
// same trick as the style cache), which width, which fraction step.
type barKey struct {
	a, b styleKey
	w, s int
	kind uint8 // 0 = GradientBar fill, 1 = DualBar
}

var (
	barMu    sync.Mutex
	barCache = map[barKey]string{}
)

// cacheBar returns the memoized fill for key, rendering it with build
// on a miss and bounding the cache against pathological churn (theme
// switches create fresh ramps; the reset keeps the map tiny either way).
func cacheBar(key barKey, build func() string) string {
	barMu.Lock()
	s, ok := barCache[key]
	barMu.Unlock()
	if ok {
		return s
	}
	s = build()
	barMu.Lock()
	if len(barCache) >= 1024 {
		barCache = map[barKey]string{}
	}
	barCache[key] = s
	barMu.Unlock()
	return s
}

// GradientBar renders a btop-style meter: the filled part interpolates
// across the ramp along the bar's length (so long bars sweep green →
// red by position, not by threshold), the remainder renders dim. The
// edge cell uses an eighth block for sub-cell precision. Fills are
// memoized per (ramp, width, fraction-step) for typical meter widths —
// live values repeat far more often than they change, so most frames
// reuse the string. The fraction is quantized BEFORE the geometry is
// derived, so a cache step always renders exactly one bar shape.
func GradientBar(width int, frac float64, ramp Ramp, empty lipgloss.Style) string {
	if width < 1 {
		return ""
	}
	frac = min(max(frac, 0), 1)
	step := int(frac*barSteps + 0.5)
	eighths := int(float64(step)/barSteps*float64(width)*8 + 0.5)
	full, rem := eighths/8, eighths%8
	if full > width { // rounding overflow at frac==1
		full, rem = width, 0
	}
	pad := width - full - min(rem, 1)

	var fill string
	if width <= barCacheMaxWidth {
		fill = cacheBar(barKey{a: ramp.key(), w: width, s: step}, func() string {
			return renderBarFill(width, full, rem, ramp.styles())
		})
	} else { // a full-width meter is one bar per frame — not worth caching
		fill = renderBarFill(width, full, rem, ramp.styles())
	}
	if pad > 0 {
		return fill + empty.Render(strings.Repeat("░", pad))
	}
	return fill
}

// barCacheMaxWidth caps which meters are memoized; the one full-panel
// CPU meter renders fresh, everything below this is cache-fed.
const barCacheMaxWidth = 48

// renderBarFill builds the filled portion of a gradient meter: full
// blocks swept across the ramp plus the eighth-block edge cell.
func renderBarFill(width, full, rem int, styles [styleSteps]lipgloss.Style) string {
	var sb strings.Builder
	for i := 0; i < full; i++ {
		t := (float64(i) + 0.5) / float64(width)
		sb.WriteString(styleFor(styles, t).Render("█"))
	}
	if rem > 0 {
		t := (float64(full) + float64(rem)/8) / float64(width)
		sb.WriteString(styleFor(styles, t).Render(string(eighthBlocks[rem-1])))
	}
	return sb.String()
}

// styleFor picks the nearest cached style for t ∈ [0,1]; an empty
// cache (empty ramp) degrades to an unstyled render.
func styleFor(styles [styleSteps]lipgloss.Style, t float64) lipgloss.Style {
	if len(styles) == 0 {
		return lipgloss.NewStyle()
	}
	i := int(min(max(t, 0), 1)*float64(styleSteps-1) + 0.5)
	return styles[i]
}

// sparkBlocks are the vertical fill runes from empty to full for
// one-line sparklines.
var sparkBlocks = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// key pins the ramp instance for the derived-style caches: theme-owned
// ramps are stable slices, so the backing-array pointer identifies the
// instance without hashing colors on every lookup.
func (r Ramp) key() styleKey {
	if len(r) == 0 {
		return styleKey{}
	}
	return styleKey{p: &r[0], n: len(r)}
}

// DualBar renders a glances-style mixed meter of width cells: the first
// frac of the bar interpolates across rampA and the remainder across
// rampB, so a single always-full bar visualizes a direction split
// (download vs upload, read vs write) while the magnitude lives in the
// neighboring rate text. Whole bars are memoized per
// (rampA, rampB, width, fraction-step).
func DualBar(width int, frac float64, rampA, rampB Ramp) string {
	if width < 1 {
		return ""
	}
	frac = min(max(frac, 0), 1)
	step := int(frac*barSteps + 0.5)
	aWidth := min(int(float64(step)/barSteps*float64(width)+0.5), width)

	return cacheBar(barKey{a: rampA.key(), b: rampB.key(), w: width, s: step, kind: 1}, func() string {
		stylesA, stylesB := rampA.styles(), rampB.styles()
		var sb strings.Builder
		for i := range width {
			styles := stylesB
			if i < aWidth {
				styles = stylesA
			}
			t := (float64(i) + 0.5) / float64(width)
			sb.WriteString(styleFor(styles, t).Render("█"))
		}
		return sb.String()
	})
}

// Sparkline renders the values (each 0..1) as one row of block glyphs,
// colored per glyph by ramping on the value.
func Sparkline(values []float64, ramp Ramp) string {
	styles := ramp.styles()
	var sb strings.Builder
	for _, v := range values {
		v = min(max(v, 0), 1)
		idx := int(v*float64(len(sparkBlocks)-1) + 0.5)
		if idx >= len(sparkBlocks) {
			idx = len(sparkBlocks) - 1
		}
		sb.WriteString(styleFor(styles, v).Render(string(sparkBlocks[idx])))
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
