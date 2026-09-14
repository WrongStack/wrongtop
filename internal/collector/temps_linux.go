//go:build linux

package collector

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// hwmonRoot is where the kernel exposes hwmon chips; a package var so
// tests can point the decoder at a synthetic sysfs tree.
var hwmonRoot = "/sys/class/hwmon"

// platformTemps reports CPU and thermal-zone temperatures from hwmon
// (the standard place kernel drivers expose tempN_input millidegree
// readings: coretemp on Intel, k10temp on AMD, acpitz ACPI zones).
// Chips without readable inputs contribute nothing; a machine with no
// hwmon temperature support yields nil. Only CPU-relevant sensors are
// returned — the arm contract TestCollectSensorsFiltersCPURelevant
// enforces (drive sensors like an NVMe "Composite" are excluded).
func platformTemps(_ context.Context) []Sensor {
	matches, _ := filepath.Glob(filepath.Join(hwmonRoot, "hwmon*", "temp*_input"))
	var out []Sensor
	for _, in := range matches {
		raw, err := os.ReadFile(in)
		if err != nil {
			continue
		}
		milli, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
		if err != nil {
			continue
		}
		t := float64(milli) / 1000 //nolint:mnd // hwmon reports millidegrees
		if t <= 0 || t > 120 {     //nolint:mnd // sane physical bounds, as on windows
			continue
		}
		name := hwmonTempName(in)
		if !sensorRelevant(name) { // NVMe/drive sensors ("Composite") are not CPU
			continue
		}
		out = append(out, Sensor{Name: name, TempC: t})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// hwmonTempName prefers the human label ("Core 0", "Tctl") and falls
// back to the chip name plus input index ("coretemp temp2") — both
// shapes carry a CPU-relevant sensorHints match.
func hwmonTempName(input string) string {
	dir := filepath.Dir(input)
	base := strings.TrimSuffix(filepath.Base(input), "_input")
	if label, err := os.ReadFile(filepath.Join(dir, base+"_label")); err == nil {
		if s := strings.TrimSpace(string(label)); s != "" {
			return s
		}
	}
	if chip, err := os.ReadFile(filepath.Join(dir, "name")); err == nil {
		if s := strings.TrimSpace(string(chip)); s != "" {
			return s + " " + base
		}
	}
	return base
}
