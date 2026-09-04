//go:build linux

package collector

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// zramStats reports the total capacity and the stored (uncompressed) size
// of every zram device from sysfs. Unconfigured devices (disksize 0) are
// skipped; on systems without zram both values are 0.
func zramStats() (total, used uint64) {
	devices, _ := filepath.Glob("/sys/block/zram*/disksize")
	for _, d := range devices {
		raw, err := os.ReadFile(d)
		if err != nil {
			continue
		}
		size, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
		if err != nil || size == 0 {
			continue
		}
		stat, err := os.ReadFile(filepath.Join(filepath.Dir(d), "mm_stat"))
		if err != nil {
			continue
		}
		fields := strings.Fields(string(stat))
		if len(fields) < 1 { //nolint:mnd // orig_data_size is the first column
			continue
		}
		orig, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		total += size
		used += orig
	}
	return total, used
}
