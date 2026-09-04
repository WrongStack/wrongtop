//go:build linux

package collector

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// readFans reports cooling fan speeds from hwmon (the standard place
// kernel drivers expose fanN_input RPM counters).
func readFans() []Fan {
	matches, _ := filepath.Glob("/sys/class/hwmon/hwmon*/fan*_input")
	var fans []Fan
	for _, m := range matches {
		raw, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		rpm, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
		if err != nil || rpm <= 0 {
			continue
		}
		dir := filepath.Base(filepath.Dir(m)) // hwmon0
		idx := strings.TrimSuffix(filepath.Base(m), "_input")
		name := dir + " " + idx
		if label, err := os.ReadFile(filepath.Join(filepath.Dir(m), strings.TrimSuffix(filepath.Base(m), "_input")+"_label")); err == nil {
			name = strings.TrimSpace(string(label))
		}
		fans = append(fans, Fan{Name: name, RPM: rpm})
	}
	sort.Slice(fans, func(i, j int) bool { return fans[i].Name < fans[j].Name })
	return fans[:min(len(fans), 8)]
}
