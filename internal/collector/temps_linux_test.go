//go:build linux

package collector

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestPlatformTempsHwmon drives the hwmon decoder against a synthetic
// sysfs tree: millidegree conversion, the (0,120]°C plausibility
// window, label-preferred naming, the chip-name fallback, and the
// CPU-relevance filter that keeps drive sensors like an NVME
// "Composite" out of the CPU temperature list.
func TestPlatformTempsHwmon(t *testing.T) {
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
	// coretemp: labeled core and package sensors
	write("hwmon0/name", "coretemp\n")
	write("hwmon0/temp1_input", "45000\n") // 45.0°C
	write("hwmon0/temp1_label", "Core 0\n")
	write("hwmon0/temp2_input", "52000\n") // 52.0°C
	write("hwmon0/temp2_label", "Package id 0\n")
	// k10temp: unlabeled → chip-name fallback keeps it hint-matchable
	write("hwmon1/name", "k10temp\n")
	write("hwmon1/temp1_input", "61437\n") // 61.437°C
	// flaky chip: every rejection path in one place
	write("hwmon2/name", "flaky\n")
	write("hwmon2/temp1_input", "-220\n")   // negative
	write("hwmon2/temp2_input", "0\n")      // missing
	write("hwmon2/temp3_input", "150000\n") // 150°C, over the window
	write("hwmon2/temp4_input", "garbage\n")
	// in-bounds but not CPU: NVMe drives label their sensor "Composite"
	write("hwmon2/temp5_input", "41000\n") // 41.0°C
	write("hwmon2/temp5_label", "Composite\n")
	// k10temp labels its sensor Tctl on AMD — matched via the sensorHints
	// entry, not the label text alone
	write("hwmon3/name", "k10temp\n")
	write("hwmon3/temp1_input", "77000\n") // 77.0°C
	write("hwmon3/temp1_label", "Tctl\n")

	orig := hwmonRoot
	hwmonRoot = root
	t.Cleanup(func() { hwmonRoot = orig })

	temps := platformTemps(context.Background())
	if len(temps) != 4 {
		t.Fatalf("got %+v, want 4 sensors", temps)
	}
	want := map[string]float64{
		"Core 0":        45,
		"Package id 0":  52,
		"Tctl":          77,
		"k10temp temp1": 61.437,
	}
	for _, s := range temps {
		w, ok := want[s.Name]
		if !ok {
			t.Errorf("unexpected sensor %q (%v°C)", s.Name, s.TempC)
			continue
		}
		if math.Abs(s.TempC-w) > 0.001 {
			t.Errorf("%s: got %v°C, want %v°C", s.Name, s.TempC, w)
		}
	}
}
