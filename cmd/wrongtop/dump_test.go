package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wrongstack/wrongtop/internal/collector"
)

// TestDumpCountExitsPromptly pins the dump contract: --count N bounds
// snapshots, not seconds. After emitting the last requested snapshot
// the command must exit without waiting another refresh interval — the
// inter-snapshot sleep paces between snapshots, and there is no next
// one. Without this, scripts pay one extra refresh per dump invocation.
//
// The bound uses the 10s clamp-max refresh so the assertion is immune
// to first-Collect cost (~0.5s on this host) while a surviving trailing
// sleep is unmissable (>= 10s).
func TestDumpCountExitsPromptly(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfg, []byte("refresh: 10s\n"), 0o600); err != nil {
		t.Fatalf("FAIL: write config: %v", err)
	}

	start := time.Now()
	root := newRootCmd()
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"dump", "-c", cfg, "--count", "1"})
	if err := root.Execute(); err != nil {
		t.Fatalf("FAIL: dump: %v", err)
	}
	elapsed := time.Since(start)

	// sanity: the one requested snapshot was produced and is valid —
	// the failure below must be about exit latency, not missing output
	dec := json.NewDecoder(strings.NewReader(out.String()))
	var snap collector.Snapshot
	if err := dec.Decode(&snap); err != nil {
		t.Fatalf("FAIL: decode snapshot: %v", err)
	}
	if snap.Time.IsZero() {
		t.Fatalf("FAIL: zero-timestamp snapshot")
	}

	if elapsed >= 5*time.Second {
		t.Fatalf("FAIL: dump --count 1 took %v with refresh 10s: the loop sleeps after the final snapshot before exiting", elapsed)
	}
}
