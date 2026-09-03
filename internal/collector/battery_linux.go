//go:build linux

package collector

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// readBattery reads the first battery from sysfs. A nil result without
// error means no battery is present.
func readBattery(ctx context.Context) *Battery {
	entries, err := os.ReadDir("/sys/class/power_supply")
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !strings.HasPrefix(strings.ToUpper(e.Name()), "BAT") {
			continue
		}
		dir := filepath.Join("/sys/class/power_supply", e.Name())
		cap, err := os.ReadFile(filepath.Join(dir, "capacity"))
		if err != nil {
			continue
		}
		pct, err := strconv.ParseFloat(strings.TrimSpace(string(cap)), 64)
		if err != nil {
			continue
		}
		charging := false
		if st, err := os.ReadFile(filepath.Join(dir, "status")); err == nil {
			charging = !strings.EqualFold(strings.TrimSpace(string(st)), "discharging")
		}
		return &Battery{Percent: pct, Charging: charging}
	}
	return nil
}
