//go:build darwin

package collector

import (
	"encoding/binary"
	"os"
)

// utmpx constants and record layout for macOS (/usr/include/utmpx.h):
// ut_user[256] at 0, ut_id[4] at 256, ut_line[32] at 260, ut_pid at 292,
// ut_type (u16) at 296. Record size 628 — verified empirically.
const (
	utmpxRecordSize  = 628
	utmpxUserOffset  = 0
	utmpxUserSize    = 256
	utmpxTypeOffset  = 296
	utmpxUserProcess = 7
)

// utmpxPath is indirected for tests: the real database only exists on a
// live multi-user system.
var utmpxPath = "/var/run/utmpx"

// countUsers returns the number of distinct logged-in users from the
// utmpx database.
func countUsers() int {
	raw, err := os.ReadFile(utmpxPath)
	if err != nil {
		return 0
	}
	seen := make(map[string]bool)
	for off := 0; off+utmpxRecordSize <= len(raw); off += utmpxRecordSize {
		rec := raw[off : off+utmpxRecordSize]
		if binary.LittleEndian.Uint16(rec[utmpxTypeOffset:]) != utmpxUserProcess {
			continue
		}
		if name := cstrField(rec[utmpxUserOffset : utmpxUserOffset+utmpxUserSize]); name != "" {
			seen[name] = true
		}
	}
	return len(seen)
}
