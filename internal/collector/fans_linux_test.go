//go:build linux

package collector

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestReadFansHwmon drives the hwmon fan decoder against a synthetic
// sysfs tree: label-preferred naming, the chip+index fallback, and the
// zero-RPM / unparsable drop paths.
func TestReadFansHwmon(t *testing.T) {
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
	write("hwmon0/name", "nct6775\n")
	write("hwmon0/fan1_input", "1250\n")
	write("hwmon0/fan1_label", "CPU Fan\n")
	write("hwmon0/fan2_input", "0\n")       // spun down → dropped
	write("hwmon0/fan3_input", "garbage\n") // unparsable → dropped
	write("hwmon1/fan1_input", "900\n")     // unlabeled → chip + index name

	orig := hwmonRoot
	hwmonRoot = root
	t.Cleanup(func() { hwmonRoot = orig })

	fans := readFans()
	if len(fans) != 2 {
		t.Fatalf("got %+v, want 2 fans", fans)
	}
	want := map[string]float64{
		"CPU Fan":     1250,
		"hwmon1 fan1": 900,
	}
	for _, f := range fans {
		w, ok := want[f.Name]
		if !ok {
			t.Errorf("unexpected fan %q (%v rpm)", f.Name, f.RPM)
			continue
		}
		if f.RPM != w {
			t.Errorf("%s: got %v rpm, want %v", f.Name, f.RPM, w)
		}
	}
}

// TestReadFansCapsAtEight pins the 8-fan ceiling: a board exposing more
// sensors than that must not inflate the sensors tab.
func TestReadFansCapsAtEight(t *testing.T) {
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
	for i := 1; i <= 10; i++ {
		write("hwmon0/fan"+strconv.Itoa(i)+"_input", strconv.Itoa(1000+i)+"\n")
		write("hwmon0/fan"+strconv.Itoa(i)+"_label", "Fan "+string(rune('A'+i-1))+"\n")
	}

	orig := hwmonRoot
	hwmonRoot = root
	t.Cleanup(func() { hwmonRoot = orig })

	fans := readFans()
	if len(fans) != 8 {
		t.Fatalf("got %d fans, want the 8-fan cap", len(fans))
	}
	for i, f := range fans { // sorted by name: Fan A..Fan H survive
		if want := "Fan " + string(rune('A'+i)); f.Name != want {
			t.Fatalf("position %d = %q, want %q", i, f.Name, want)
		}
	}
}
