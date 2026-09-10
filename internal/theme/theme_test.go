package theme

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/wrongstack/wrongtop/internal/ui/canvas"
)

func TestResolveBuiltins(t *testing.T) {
	for _, name := range PaletteNames() {
		if _, ok := Resolve(name); !ok {
			t.Errorf("builtin %q not resolvable", name)
		}
	}
	if _, ok := Resolve("no-such-theme"); ok {
		t.Error("unknown theme should not resolve")
	}
	if got := ByName("no-such-theme").Palette.Name; got != TokyoNight.Name {
		t.Errorf("ByName fallback = %q, want %q", got, TokyoNight.Name)
	}
}

// TestRampsCoverAllRoles pins the shared value gradients: every role is
// populated after New so tabs never fall back to ad-hoc ramps.
func TestRampsCoverAllRoles(t *testing.T) {
	th := ByName("tokyo-night")
	for name, r := range map[string]canvas.Ramp{
		"cpu":  th.Ramps.CPU,
		"mem":  th.Ramps.Mem,
		"swap": th.Ramps.Swap,
		"io":   th.Ramps.IO,
		"bat":  th.Ramps.Bat,
		"load": th.Ramps.Load,
		"rx":   th.Ramps.RX,
		"tx":   th.Ramps.TX,
	} {
		if len(r) < 2 {
			t.Errorf("ramp %q too short: %v", name, r)
			continue
		}
		for _, hex := range r {
			if len(hex) != 7 || hex[0] != '#' {
				t.Errorf("ramp %q has malformed color %q", name, hex)
			}
		}
	}
}

// TestNoDuplicateAccents guards the palette quirks where two roles
// shared one hex: adjacent panels (HOST blue vs NETWORK cyan) rendered
// identical borders.
func TestNoDuplicateAccents(t *testing.T) {
	for _, p := range []Palette{Dracula, RosePine, TokyoNight, GruvboxDark} {
		if p.Blue == p.Cyan {
			t.Errorf("%s: blue and cyan share %s", p.Name, p.Blue)
		}
		if p.Cyan == p.Orange {
			t.Errorf("%s: cyan and orange share %s", p.Name, p.Cyan)
		}
	}
}

func TestNamesIncludesBuiltinsAndUsers(t *testing.T) {
	// seed the user registry without touching the filesystem
	userMu.Lock()
	userPalettes = map[string]Palette{"my-theme": {Name: "my-theme", BG: "#123456"}}
	userLoaded = true
	userMu.Unlock()
	t.Cleanup(func() {
		userMu.Lock()
		userPalettes = nil
		userLoaded = false
		userMu.Unlock()
	})

	names := Names()
	found := false
	for _, n := range names {
		if n == "my-theme" {
			found = true
		}
	}
	if !found {
		t.Errorf("Names() missing user theme: %v", names)
	}
	if p, ok := Resolve("my-theme"); !ok || p.BG != "#123456" {
		t.Errorf("Resolve(my-theme) = %+v, %v", p, ok)
	}
}

func TestLoadUserThemes(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("ocean.yml", "name: ocean\nbg: \"#001122\"\nfg: \"#aaccee\"\nred: \"#ff0000\"\n")
	write("broken.yml", "not: [valid\n")

	if err := LoadUserThemes(dir); err != nil {
		t.Fatalf("LoadUserThemes: %v", err)
	}
	p, ok := Resolve("ocean")
	if !ok {
		t.Fatal("user theme ocean not loaded")
	}
	if p.BG != "#001122" || p.Red != "#ff0000" {
		t.Errorf("ocean palette colors wrong: %+v", p)
	}
	if p.Green != GruvboxDark.Green {
		t.Errorf("unset slot should inherit the default, got %q", p.Green)
	}
	if _, ok := Resolve("broken"); ok {
		t.Error("unparsable theme file must be skipped")
	}
}

// TestNoDeadlock guards the reentrancy regression: Resolve must not
// block when it triggers the lazy user-theme load.
func TestNoDeadlock(t *testing.T) {
	done := make(chan struct{})
	go func() {
		_ = ByName("whatever")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ByName deadlocked on the lazy user-theme load")
	}
}

// resetUserThemes restores the global user-palette registry after a test
// that loaded themes, so later tests start from the lazy-load path.
func resetUserThemes(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		userMu.Lock()
		userPalettes = nil
		userLoaded = false
		userMu.Unlock()
	})
}

func TestThemesDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".config", "wrongtop", "themes")
	if got := ThemesDir(); got != want {
		t.Errorf("ThemesDir() = %q, want %q", got, want)
	}
	t.Setenv("HOME", "")
	if got := ThemesDir(); got != "" {
		t.Errorf("ThemesDir() without home = %q, want empty", got)
	}
}

// TestLoadUserThemesEmptyDir pins the no-op: an empty dir clears and
// marks the registry loaded without touching the filesystem.
func TestLoadUserThemesEmptyDir(t *testing.T) {
	resetUserThemes(t)
	if err := LoadUserThemes(""); err != nil {
		t.Fatalf("LoadUserThemes(\"\"): %v", err)
	}
	if names := Names(); len(names) != len(PaletteNames()) {
		t.Errorf("Names() = %d entries, want only the %d built-ins",
			len(names), len(PaletteNames()))
	}
}

// TestLoadUserThemesGlobError covers the malformed-pattern branch: a
// directory whose name holds an unterminated character class makes
// filepath.Glob fail.
func TestLoadUserThemesGlobError(t *testing.T) {
	resetUserThemes(t)
	bad := filepath.Join(t.TempDir(), "[weird")
	if err := os.MkdirAll(bad, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := LoadUserThemes(bad); err == nil {
		t.Error("malformed glob pattern should error")
	}
}

// TestLoadUserThemesSkipsUnreadable covers the read-error continue: a
// *.yml entry that cannot be read (dangling symlink) is skipped without
// aborting the remaining files.
func TestLoadUserThemesSkipsUnreadable(t *testing.T) {
	resetUserThemes(t)
	dir := t.TempDir()
	good := filepath.Join(dir, "good.yml")
	body := "name: good\nbg: \"#112233\"\n"
	if err := os.WriteFile(good, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing-target"), filepath.Join(dir, "ghost.yml")); err != nil {
		t.Fatal(err)
	}
	if err := LoadUserThemes(dir); err != nil {
		t.Fatalf("LoadUserThemes: %v", err)
	}
	if _, ok := Resolve("good"); !ok {
		t.Error("good.yml should load despite the unreadable sibling")
	}
	if _, ok := Resolve("ghost"); ok {
		t.Error("unreadable ghost.yml must be skipped")
	}
}

// TestSoft pins the background blend used by chips and alert strips.
func TestSoft(t *testing.T) {
	cases := []struct {
		p    Palette
		hex  string
		want string
	}{
		{GruvboxDark, "#ebdbb2", "#938a74"},
		{TokyoNight, "#c0caf5", "#757b98"},
		{GruvboxDark, "#282828", "#282828"}, // blending the bg into itself is a no-op
		{GruvboxDark, "#fb4934", "#9c3a2f"},
	}
	for _, c := range cases {
		th := New(c.p)
		if got := th.Soft(c.hex); got != c.want {
			t.Errorf("Soft(%q) on %s = %q, want %q", c.hex, c.p.Name, got, c.want)
		}
	}
}

// TestDerivedBlendHexValid pins the blends theme.New and Soft feed into
// lipgloss.Color: with a plausible user theme whose background is black,
// these blends once produced a red channel below 0x10, which truncated
// the hex string to 5 digits and silently dropped the derived color.
func TestDerivedBlendHexValid(t *testing.T) {
	var hexShape = regexp.MustCompile(`^#[0-9a-f]{6}$`)
	palettes := []Palette{{
		Name: "black-bg", BG: "#000000", FG: "#e0e0e0",
		Red: "#ff5555", Green: "#50fa7b", Yellow: "#f1fa8c",
		Blue: "#6272a4", Purple: "#bd93f9", Cyan: "#8be9fd",
		Orange: "#ffb86c", Gray: "#1e1e1e",
	}}
	for _, name := range PaletteNames() {
		if p, ok := Resolve(name); ok {
			palettes = append(palettes, p)
		}
	}
	for _, p := range palettes {
		// the exact expressions New builds for the derived backgrounds
		blends := []struct {
			what string
			hex  string
		}{
			{"TabActive", canvas.Ramp{p.BG, p.Purple}.At(0.55)},
			{"Track", canvas.Ramp{p.BG, p.Gray}.At(0.45)},
			{"Selected", canvas.Ramp{p.BG, p.Purple}.At(0.30)},
			{"Soft(FG)", New(p).Soft(p.FG)},
		}
		for _, b := range blends {
			if !hexShape.MatchString(b.hex) {
				t.Errorf("%s: %s blend = %q, want a #rrggbb hex color", p.Name, b.what, b.hex)
			}
		}
	}
}

// TestValueThresholds pins the warn/crit banding: warn and crit are
// inclusive lower bounds.
func TestValueThresholds(t *testing.T) {
	th := New(TokyoNight)
	cases := []struct {
		pct  float64
		want lipgloss.Style
	}{
		{0, th.Styles.OK},
		{69.9, th.Styles.OK},
		{70, th.Styles.Warn},
		{89.9, th.Styles.Warn},
		{90, th.Styles.Crit},
		{100, th.Styles.Crit},
	}
	for _, c := range cases {
		if got := th.Value(70, 90, c.pct); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Value(70, 90, %v) returned a different style", c.pct)
		}
	}
	// the three bands must be visually distinct styles
	if reflect.DeepEqual(th.Styles.OK, th.Styles.Warn) ||
		reflect.DeepEqual(th.Styles.Warn, th.Styles.Crit) {
		t.Error("OK/Warn/Crit styles should differ")
	}
}
