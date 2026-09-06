package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNormalizeInvalidPercentThresholds pins the normalize contract for
// the percent thresholds: non-positive warns and crit-not-above-warn
// fall back to the defaults, mirroring the temp-threshold handling —
// otherwise a config typo (cpu_warn: 0, swapped pairs, negatives) makes
// every snapshot open permanent cpu/mem/swap/disk alerts.
func TestNormalizeInvalidPercentThresholds(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		t.Helper()
		p := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("FAIL: write config: %v", err)
		}
		return p
	}

	cases := []struct {
		name string
		yaml string
		want Thresholds
	}{
		{
			name: "zero warn falls back to defaults",
			yaml: "thresholds:\n  cpu_warn: 0\n  mem_warn: 0\n",
			want: Thresholds{CPUWarn: 70, CPUCrit: 90, MemWarn: 80, MemCrit: 95, TempWarn: 60, TempCrit: 80},
		},
		{
			name: "negative warn falls back to default",
			yaml: "thresholds:\n  cpu_warn: -5\n",
			want: Thresholds{CPUWarn: 70, CPUCrit: 90, MemWarn: 80, MemCrit: 95, TempWarn: 60, TempCrit: 80},
		},
		{
			name: "crit not above warn falls back to default crit",
			yaml: "thresholds:\n  cpu_warn: 85\n  cpu_crit: 50\n  mem_warn: 90\n  mem_crit: 90\n",
			want: Thresholds{CPUWarn: 85, CPUCrit: 90, MemWarn: 90, MemCrit: 95, TempWarn: 60, TempCrit: 80},
		},
		{
			name: "valid pairs pass through unchanged",
			yaml: "thresholds:\n  cpu_warn: 55\n  cpu_crit: 66\n  mem_warn: 77\n  mem_crit: 88\n",
			want: Thresholds{CPUWarn: 55, CPUCrit: 66, MemWarn: 77, MemCrit: 88, TempWarn: 60, TempCrit: 80},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(write(tc.yaml))
			if err != nil {
				t.Fatalf("FAIL: Load: %v", err)
			}
			if cfg.Thresholds != tc.want {
				t.Fatalf("FAIL: thresholds after Load = %+v, want %+v", cfg.Thresholds, tc.want)
			}
		})
	}
}
