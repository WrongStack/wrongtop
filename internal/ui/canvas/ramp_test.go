package canvas

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

var testRamp = Ramp{"#102030", "#808080", "#ffffff"}

// Ramp.Colors adapts the hex list to the []color.Color shape Graph takes.
func TestRampColors(t *testing.T) {
	colors := testRamp.Colors()
	if len(colors) != 3 {
		t.Fatalf("Colors() len = %d, want 3", len(colors))
	}
	for i, c := range colors {
		if c == nil {
			t.Errorf("Colors()[%d] is nil, want a color for %q", i, testRamp[i])
		}
	}
}

// At interpolates between stops, clamps t, and degrades on bad colors.
func TestRampAt(t *testing.T) {
	cases := []struct {
		name string
		ramp Ramp
		at   float64
		want string
	}{
		{"empty ramp falls back to gray", Ramp{}, 0.3, "#888888"},
		{"single stop is constant", Ramp{"#00ff00"}, 0.9, "#00ff00"},
		{"below range clamps to first", testRamp, -1, "#102030"},
		{"first stop", testRamp, 0, "#102030"},
		{"mid interpolation", Ramp{"#000000", "#ffffff"}, 0.5, "#808080"},
		{"above range clamps to last", testRamp, 2, "#ffffff"},
		{"last stop", testRamp, 1, "#ffffff"},
		{"invalid first stop returned verbatim", Ramp{"nope", "#ffffff"}, 0.5, "nope"},
		{"invalid second stop keeps first", Ramp{"#000000", "zzzz"}, 0.5, "#000000"},
	}
	for _, tc := range cases {
		if got := tc.ramp.At(tc.at); got != tc.want {
			t.Errorf("%s: At(%v) = %q, want %q", tc.name, tc.at, got, tc.want)
		}
	}
}

// TestAtEmitsValidHex pins the output contract across a full sweep:
// every interpolated value must be a well-formed "#rrggbb" hex color,
// because Theme.New and Theme.Soft feed At's result straight into
// lipgloss.Color. Dark blends — a user theme with a black background —
// once truncated to 5 hex digits whenever the red channel fell below
// 0x10, silently dropping the derived color.
func TestAtEmitsValidHex(t *testing.T) {
	var hexShape = regexp.MustCompile(`^#[0-9a-f]{6}$`)
	ramps := []Ramp{
		{"#000000", "#1e1e1e"}, // user-theme pair: black bg, dark gray
		{"#000000", "#ffffff"},
		{"#102030", "#808080", "#ffffff"},
	}
	for _, r := range ramps {
		for i := 0; i <= 200; i++ {
			f := float64(i) / 200
			if got := r.At(f); !hexShape.MatchString(got) {
				t.Fatalf("At(%0.3f) on %v = %q, want a #rrggbb hex color", f, r, got)
			}
		}
	}
}

func TestParseHex(t *testing.T) {
	cases := []struct {
		in    string
		rgb   [3]uint8
		valid bool
	}{
		{"#ff8000", [3]uint8{255, 128, 0}, true},
		{"ff8000", [3]uint8{255, 128, 0}, true},
		{"#102030", [3]uint8{16, 32, 48}, true},
		{"", [3]uint8{}, false},        // too short
		{"#fff", [3]uint8{}, false},    // too short
		{"#ff80zz", [3]uint8{}, false}, // not hex digits
	}
	for _, tc := range cases {
		rgb, ok := parseHex(tc.in)
		if ok != tc.valid || rgb != tc.rgb {
			t.Errorf("parseHex(%q) = %v, %v; want %v, %v", tc.in, rgb, ok, tc.rgb, tc.valid)
		}
	}
}

func TestHexFormats(t *testing.T) {
	cases := []struct {
		r, g, b uint8
		want    string
	}{
		{255, 128, 0, "#ff8000"},
		{16, 32, 48, "#102030"},
		// red below 0x10 must keep its leading zero: a 5-digit hex is
		// rejected by lipgloss, silently dropping the derived color
		{0, 0, 0, "#000000"},
		{1, 1, 1, "#010101"},
		{14, 14, 14, "#0e0e0e"},
		{15, 0, 255, "#0f00ff"},
	}
	for _, tc := range cases {
		if got := hex(tc.r, tc.g, tc.b); got != tc.want {
			t.Errorf("hex(%d,%d,%d) = %q, want %q", tc.r, tc.g, tc.b, got, tc.want)
		}
	}
}

// StyleAt serves cached styles; empty ramps degrade to an unstyled one.
func TestStyleAtEmptyRamp(t *testing.T) {
	st := Ramp{}.StyleAt(0.5)
	if got := st.Render("x"); got != "x" {
		t.Errorf("empty ramp StyleAt should render unstyled, got %q", got)
	}
}

func TestStyleAtClamps(t *testing.T) {
	low := testRamp.StyleAt(-1)
	high := testRamp.StyleAt(2)
	if got := low.Render("x"); got != testRamp.StyleAt(0).Render("x") {
		t.Error("StyleAt(-1) should clamp to the ramp start")
	}
	if got := high.Render("x"); got != testRamp.StyleAt(1).Render("x") {
		t.Error("StyleAt(2) should clamp to the ramp end")
	}
}

// styles caches per-ramp: a miss builds and stores, a hit returns
// without growing the cache.
func TestRampStylesCache(t *testing.T) {
	r := Ramp{"#010203", "#040506"}
	key := r.key()
	styleMu.Lock()
	delete(styleCache, key)
	n0 := len(styleCache)
	styleMu.Unlock()

	r.StyleAt(0.5) // miss: builds the 48-step table
	styleMu.Lock()
	n1 := len(styleCache)
	styleMu.Unlock()
	if n1 != n0+1 {
		t.Fatalf("cache miss should add one entry: %d -> %d", n0, n1)
	}

	r.StyleAt(0.9) // hit: no growth
	styleMu.Lock()
	n2 := len(styleCache)
	styleMu.Unlock()
	if n2 != n1 {
		t.Errorf("cache hit should not grow the cache: %d -> %d", n1, n2)
	}
}

// The style cache resets once it grows past its bound, so pathological
// ramp churn cannot leak memory.
func TestRampStylesCacheReset(t *testing.T) {
	styleMu.Lock()
	styleCache = map[styleKey][styleSteps]lipgloss.Style{}
	styleMu.Unlock()

	for i := 0; i < 64; i++ {
		Ramp{fmt.Sprintf("#%06d", i)}.StyleAt(0.5)
	}
	styleMu.Lock()
	n := len(styleCache)
	styleMu.Unlock()
	if n != 64 {
		t.Fatalf("cache should hold 64 distinct ramps, has %d", n)
	}

	Ramp{"#ffffff"}.StyleAt(0.5) // 65th distinct ramp: resets, then stores one
	styleMu.Lock()
	n = len(styleCache)
	styleMu.Unlock()
	if n != 1 {
		t.Errorf("overflowing the cache should reset it to one entry, has %d", n)
	}
}

func TestRampKey(t *testing.T) {
	if got := (Ramp{}).key(); got != (styleKey{}) {
		t.Errorf("empty ramp key should be the zero key, got %+v", got)
	}
	r := Ramp{"#abcdef"}
	key := r.key()
	if key.n != 1 || key.p != &r[0] {
		t.Errorf("key should pin the backing array: %+v", key)
	}
}

func TestCacheBarHitAndMiss(t *testing.T) {
	barMu.Lock()
	barCache = map[barKey]string{}
	barMu.Unlock()

	calls := 0
	build := func() string { calls++; return "built" }
	key := barKey{w: 42, s: 7}
	if got := cacheBar(key, build); got != "built" || calls != 1 {
		t.Fatalf("first call should build: got %q after %d builds", got, calls)
	}
	if got := cacheBar(key, build); got != "built" || calls != 1 {
		t.Errorf("second call should hit the cache: got %q after %d builds", got, calls)
	}
}

func TestCacheBarReset(t *testing.T) {
	barMu.Lock()
	barCache = map[barKey]string{}
	barMu.Unlock()

	for i := 0; i < 1024; i++ {
		cacheBar(barKey{w: i, s: i}, func() string { return "x" })
	}
	barMu.Lock()
	n := len(barCache)
	barMu.Unlock()
	if n != 1024 {
		t.Fatalf("cache should hold 1024 bars, has %d", n)
	}

	cacheBar(barKey{w: 1 << 20, s: 0}, func() string { return "y" }) // over the bound
	barMu.Lock()
	n = len(barCache)
	barMu.Unlock()
	if n != 1 {
		t.Errorf("overflowing the bar cache should reset it to one entry, has %d", n)
	}
}

func TestGradientBarGeometry(t *testing.T) {
	st := lipgloss.NewStyle()
	cases := []struct {
		name  string
		width int
		frac  float64
		want  string
	}{
		{"zero width", 0, 0.5, ""},
		{"negative width", -3, 0.5, ""},
		{"empty", 4, 0, "░░░░"},
		{"full", 4, 1, "████"},
		{"half", 4, 0.5, "██░░"},
		{"eighth edge", 10, 0.25, "██▌░░░░░░░"},
		{"negative frac clamps to empty", 4, -1, "░░░░"},
		{"frac above one clamps to full", 4, 1.5, "████"},
	}
	for _, tc := range cases {
		if got := stripANSI(GradientBar(tc.width, tc.frac, testRamp, st)); got != tc.want {
			t.Errorf("%s: GradientBar(%d, %v) = %q, want %q", tc.name, tc.width, tc.frac, got, tc.want)
		}
	}
}

// Widths past the memoization bound render fresh; the geometry must not
// change with the cache decision.
func TestGradientBarUncachedWidth(t *testing.T) {
	st := lipgloss.NewStyle()
	full := stripANSI(GradientBar(50, 1, testRamp, st))
	if full != strings.Repeat("█", 50) {
		t.Errorf("uncached full bar = %q, want 50 full blocks", full)
	}
	half := stripANSI(GradientBar(50, 0.5, testRamp, st))
	if len([]rune(half)) != 50 {
		t.Errorf("uncached half bar width = %d, want 50", len([]rune(half)))
	}
}

func TestDualBar(t *testing.T) {
	a := Ramp{"#000000", "#444444"}
	b := Ramp{"#bbbbbb", "#ffffff"}

	if got := DualBar(0, 0.5, a, b); got != "" {
		t.Errorf("zero width DualBar = %q, want \"\"", got)
	}
	for _, frac := range []float64{0, 0.3, 0.5, 0.77, 1, -1, 2} {
		bar := stripANSI(DualBar(12, frac, a, b))
		if len([]rune(bar)) != 12 {
			t.Errorf("DualBar(12, %v) width = %d, want 12", frac, len([]rune(bar)))
		}
		if strings.Trim(bar, "█") != "" {
			t.Errorf("DualBar(12, %v) should be all full blocks, got %q", frac, bar)
		}
	}
}

func TestSparkline(t *testing.T) {
	ramp := testRamp
	if got := Sparkline(nil, ramp); got != "" {
		t.Errorf("empty sparkline = %q, want \"\"", got)
	}
	if got := stripANSI(Sparkline([]float64{0, 0.5, 1}, ramp)); got != " ▄█" {
		t.Errorf("Sparkline = %q, want %q", got, " ▄█")
	}
	// values outside [0,1] clamp before indexing the glyph table
	if got := stripANSI(Sparkline([]float64{-2, 9}, ramp)); got != " █" {
		t.Errorf("clamped sparkline = %q, want %q", got, " █")
	}
}

func TestSparklineScaled(t *testing.T) {
	ramp := testRamp
	if got := SparklineScaled(nil, ramp); got != "" {
		t.Errorf("empty scaled sparkline = %q, want \"\"", got)
	}
	// all-zero windows render as blanks, not divide-by-zero
	if got := SparklineScaled([]float64{0, 0, 0}, ramp); got != "   " {
		t.Errorf("all-zero window = %q, want three blanks", got)
	}
	// the tallest sample defines the top of the window
	if got := stripANSI(SparklineScaled([]float64{1, 2, 4}, ramp)); got != "▂▄█" {
		t.Errorf("scaled sparkline = %q, want %q", got, "▂▄█")
	}
}

// styleFor picks the nearest step; an empty table degrades unstyled.
func TestStyleForEmpty(t *testing.T) {
	var none [styleSteps]lipgloss.Style
	if got := styleFor(none, 0.5).Render("x"); got != "x" {
		t.Errorf("empty style table should render unstyled, got %q", got)
	}
	if got := styleFor(testRamp.styles(), 0.5).Render("x"); got == "" {
		t.Error("non-empty table should render")
	}
}
