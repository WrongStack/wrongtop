package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("WRONGTOP_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "gruvbox-dark" {
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
