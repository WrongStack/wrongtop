package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("WRONGTOP_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "tokyo-night" {
		t.Fatalf("default theme: %q", cfg.Theme)
	}
	if cfg.Refresh.D() != time.Second {
		t.Fatalf("default refresh: %v", cfg.Refresh.D())
	}
}

func TestLoadCustom(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := `
theme: dracula
refresh: 500ms
thresholds:
  cpu_warn: 60
keys:
  kill: x
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "dracula" {
		t.Fatalf("theme: %q", cfg.Theme)
	}
	if cfg.Refresh.D() != 500*time.Millisecond {
		t.Fatalf("refresh: %v", cfg.Refresh.D())
	}
	if cfg.Thresholds.CPUWarn != 60 {
		t.Fatalf("cpu_warn: %v", cfg.Thresholds.CPUWarn)
	}
	if cfg.Keys.Kill != "x" {
		t.Fatalf("kill key: %q", cfg.Keys.Kill)
	}
}

func TestRefreshClamped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("refresh: 10ms\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Refresh.D() != 250*time.Millisecond {
		t.Fatalf("refresh should clamp to 250ms, got %v", cfg.Refresh.D())
	}
}

func TestLoadInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("refresh: banana\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("invalid duration should error")
	}
}

func TestTempThresholdValidation(t *testing.T) {
	cases := []struct {
		name       string
		warn, crit float64
		wantWarn   float64
		wantCrit   float64
	}{
		{"defaults kept", 60, 80, 60, 80},
		{"zero warn falls back", 0, 80, 60, 80},
		{"crit below warn falls back", 70, 65, 70, 80},
		{"custom values kept", 55, 75, 55, 75},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			cfg.Thresholds.TempWarn, cfg.Thresholds.TempCrit = tc.warn, tc.crit
			cfg.normalize()
			if cfg.Thresholds.TempWarn != tc.wantWarn {
				t.Errorf("temp_warn: got %v, want %v", cfg.Thresholds.TempWarn, tc.wantWarn)
			}
			if cfg.Thresholds.TempCrit != tc.wantCrit {
				t.Errorf("temp_crit: got %v, want %v", cfg.Thresholds.TempCrit, tc.wantCrit)
			}
		})
	}
}

func TestKeysValidation(t *testing.T) {
	cases := []struct {
		name       string
		kill       string
		filter     string
		wantKill   string
		wantFilter string
	}{
		{"valid overrides kept", "d", "x", "d", "x"},
		{"defaults kept", "k", "/", "k", "/"},
		{"empty falls back", "", "", "k", "/"},
		{"multi-char falls back", "ctrl+k", "ab", "k", "/"},
		{"non-letter falls back", "1", "#", "k", "/"},
		{"reserved sort keys fall back", "s", "S", "k", "/"},
		{"reserved tree key falls back", "t", "x", "k", "x"},
		{"reserved tree filter falls back", "d", "t", "d", "/"},
		{"reserved uppercase tree falls back", "T", "x", "k", "x"},
		{"reserved alerts key falls back", "a", "x", "k", "x"},
		{"reserved alerts filter falls back", "d", "a", "d", "/"},
		{"reserved quit key falls back", "q", "x", "k", "x"},
		{"reserved reload key falls back", "R", "x", "k", "x"},
		{"reserved reload filter falls back", "d", "R", "d", "/"},
		{"exact collision falls back", "x", "x", "k", "/"},
		{"case-insensitive collision falls back", "d", "D", "k", "/"},
		{"uppercase kill kept", "D", "x", "D", "x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			cfg.Keys = Keys{Kill: tc.kill, Filter: tc.filter}
			cfg.normalize()
			if cfg.Keys.Kill != tc.wantKill {
				t.Errorf("kill: got %q, want %q", cfg.Keys.Kill, tc.wantKill)
			}
			if cfg.Keys.Filter != tc.wantFilter {
				t.Errorf("filter: got %q, want %q", cfg.Keys.Filter, tc.wantFilter)
			}
		})
	}
}

func TestLoadInvalidKeysFallBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "keys:\n  kill: ctrl+k\n  filter: s\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Keys.Kill != "k" {
		t.Errorf("multi-char kill should fall back to k, got %q", cfg.Keys.Kill)
	}
	if cfg.Keys.Filter != "/" {
		t.Errorf("reserved filter should fall back to /, got %q", cfg.Keys.Filter)
	}
}

func TestPathResolution(t *testing.T) {
	t.Run("env override wins", func(t *testing.T) {
		envPath := filepath.Join(t.TempDir(), "custom.yaml")
		t.Setenv("WRONGTOP_CONFIG", envPath)
		if got := Path(); got != envPath {
			t.Errorf("Path() = %q, want %q", got, envPath)
		}
	})
	t.Run("home default", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("WRONGTOP_CONFIG", "")
		// os.UserHomeDir reads $HOME on Unix but $USERPROFILE on
		// Windows — set both so the resolution is hermetic everywhere.
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		want := filepath.Join(home, ".config", "wrongtop", "config.yaml")
		if got := Path(); got != want {
			t.Errorf("Path() = %q, want %q", got, want)
		}
	})
	t.Run("no home yields empty", func(t *testing.T) {
		t.Setenv("WRONGTOP_CONFIG", "")
		t.Setenv("HOME", "")
		t.Setenv("USERPROFILE", "")
		if got := Path(); got != "" {
			t.Errorf("Path() with no home = %q, want empty", got)
		}
	})
}

// TestLoadNoLocation covers Load("") when even the default path is
// unavailable: the built-in defaults come back untouched.
func TestLoadNoLocation(t *testing.T) {
	t.Setenv("WRONGTOP_CONFIG", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "tokyo-night" {
		t.Errorf("theme: %q", cfg.Theme)
	}
	if cfg.Refresh.D() != time.Second {
		t.Errorf("refresh: %v", cfg.Refresh.D())
	}
}

// TestLoadReadError covers read failures that are not "file missing":
// reading a directory fails with EISDIR.
func TestLoadReadError(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err == nil {
		t.Fatal("reading a directory should error")
	}
	if cfg != nil {
		t.Errorf("config should be nil on read error, got %+v", cfg)
	}
	if !strings.Contains(err.Error(), "wrongtop: reading config") {
		t.Errorf("error = %v, want reading-config prefix", err)
	}
}

// TestUnmarshalYAMLRejectsNonString pins the Duration decoder: a YAML
// node that is not a string must fail inside node.Decode.
func TestUnmarshalYAMLRejectsNonString(t *testing.T) {
	for _, doc := range []string{"12\n", "true\n", "- 1s\n- 2s\n", "a: b\n"} {
		var d Duration
		if err := yaml.Unmarshal([]byte(doc), &d); err == nil {
			t.Errorf("yaml %q should not decode into Duration", doc)
		}
	}
	var d Duration
	if err := yaml.Unmarshal([]byte("500ms\n"), &d); err != nil {
		t.Fatalf("valid duration: %v", err)
	}
	if d.D() != 500*time.Millisecond {
		t.Errorf("duration = %v, want 500ms", d.D())
	}
}

func TestNormalizeClampsAndFallbacks(t *testing.T) {
	cases := []struct {
		name        string
		refresh     time.Duration
		theme       string
		layout      string
		border      string
		wantRefresh time.Duration
		wantTheme   string
		wantLayout  string
		wantBorder  string
	}{
		{"defaults kept", time.Second, "dracula", "compact", "square",
			time.Second, "dracula", "compact", "square"},
		{"slow refresh clamps", 11 * time.Second, "nord", "full", "rounded",
			10 * time.Second, "nord", "full", "rounded"},
		{"unknown layout falls back", time.Second, "nord", "bogus", "square",
			time.Second, "nord", "full", "square"},
		{"unknown border falls back", time.Second, "nord", "minimal", "bogus",
			time.Second, "nord", "minimal", "rounded"},
		{"empty theme falls back", time.Second, "", "minimal", "thick",
			time.Second, "tokyo-night", "minimal", "thick"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			cfg.Refresh = Duration(tc.refresh)
			cfg.Theme = tc.theme
			cfg.Layout = tc.layout
			cfg.Border = tc.border
			cfg.normalize()
			if cfg.Refresh.D() != tc.wantRefresh {
				t.Errorf("refresh: got %v, want %v", cfg.Refresh.D(), tc.wantRefresh)
			}
			if cfg.Theme != tc.wantTheme {
				t.Errorf("theme: got %q, want %q", cfg.Theme, tc.wantTheme)
			}
			if cfg.Layout != tc.wantLayout {
				t.Errorf("layout: got %q, want %q", cfg.Layout, tc.wantLayout)
			}
			if cfg.Border != tc.wantBorder {
				t.Errorf("border: got %q, want %q", cfg.Border, tc.wantBorder)
			}
		})
	}
}
