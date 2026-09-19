package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/theme"
)

// TestReloadConfigRereadsUserThemeFiles pins the theme half of the
// reload contract: README promises `R` hot-reloads "theme" and themes
// are built-ins plus user palette files, so reloadConfig must re-read
// the themes dir before resolving cfg.Theme — an edited palette file
// must apply, a newly dropped one must resolve (not fall back to
// tokyo-night). The built-in dimension is covered by
// TestReloadConfigPreservesModuleSet; this covers the user-file
// dimension that the lazy startup registry otherwise freezes for the
// process lifetime.
func TestReloadConfigRereadsUserThemeFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads this on windows

	themesDir := filepath.Join(home, ".config", "wrongtop", "themes")
	if err := os.MkdirAll(themesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Reset the package-global registry when done so later tests in the
	// package are not polluted by this test's temp palettes.
	t.Cleanup(func() { _ = theme.LoadUserThemes("") })

	writeTheme := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(themesDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath := filepath.Join(home, "config.yaml")
	writeCfg := func(content string) {
		t.Helper()
		if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Precondition: startup resolves the user palette (fresh-process
	// semantics: the lazy registry load sees the current themes dir).
	writeTheme("ocean.yml", "fg: \"#111111\"\n")
	_ = theme.LoadUserThemes(theme.ThemesDir())
	writeCfg("theme: ocean\n")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	m := New(cfg, cfgPath, "test")
	if m.theme.Palette.Name != "ocean" || m.theme.Palette.FG != "#111111" {
		t.Fatalf("precondition: startup must resolve the user palette, got %s fg=%s",
			m.theme.Palette.Name, m.theme.Palette.FG)
	}

	// An edited palette file must apply on reload.
	writeTheme("ocean.yml", "fg: \"#222222\"\n")
	m.reloadConfig()
	if m.theme.Palette.Name != "ocean" || m.theme.Palette.FG != "#222222" {
		t.Fatalf("reload kept stale user-theme colors: got %s fg=%s, want ocean #222222",
			m.theme.Palette.Name, m.theme.Palette.FG)
	}

	// A newly dropped palette must resolve on reload instead of falling
	// back to the default theme.
	writeTheme("bay.yml", "name: bay\nfg: \"#333333\"\n")
	writeCfg("theme: bay\n")
	m.reloadConfig()
	if m.theme.Palette.Name != "bay" {
		t.Fatalf("reload could not resolve the newly added palette %q: applied %s",
			"bay", m.theme.Palette.Name)
	}
	if m.theme.Palette.FG != "#333333" {
		t.Fatalf("added palette applied with wrong colors: fg=%s", m.theme.Palette.FG)
	}

	// Control — built-ins apply on reload (mirrors
	// TestReloadConfigPreservesModuleSet; proves the harness).
	writeCfg("theme: nord\n")
	m.reloadConfig()
	if m.theme.Palette.Name != "nord" {
		t.Fatalf("control: built-in theme did not apply on reload: %s", m.theme.Palette.Name)
	}
}
