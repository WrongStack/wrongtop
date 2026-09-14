//go:build linux

package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestReadBatterySysfs drives the sysfs battery reader against a
// synthetic power_supply tree: BAT-prefix selection, first-battery
// wins, and a missing status file reading as not charging.
func TestReadBatterySysfs(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("ACAD/online", "1\n")    // AC adapter: skipped by name prefix
	write("BAT0/capacity", "87\n") // first BAT wins; no status file
	write("BAT1/capacity", "50\n")
	write("BAT1/status", "Discharging\n")

	orig := powerSupplyRoot
	powerSupplyRoot = root
	t.Cleanup(func() { powerSupplyRoot = orig })

	b := readBattery(context.Background())
	if b == nil {
		t.Fatal("no battery found")
	}
	if b.Percent != 87 {
		t.Errorf("percent: got %v, want 87", b.Percent)
	}
	if b.Charging {
		t.Error("missing status must read as not charging")
	}
}

// TestReadBatteryAbsent pins the no-battery contract: a machine with a
// power_supply class but no BAT* entries yields nil, not an error.
func TestReadBatteryAbsent(t *testing.T) {
	orig := powerSupplyRoot
	powerSupplyRoot = t.TempDir() // no BAT* entries
	t.Cleanup(func() { powerSupplyRoot = orig })

	if b := readBattery(context.Background()); b != nil {
		t.Fatalf("expected nil without batteries, got %+v", b)
	}
}
