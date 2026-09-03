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
