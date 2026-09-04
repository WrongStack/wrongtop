//go:build linux

package collector

import (
	"encoding/binary"
	"os"
)

// utmp record layout for Linux (/usr/include/bits/utmp.h): ut_type (u16)
// at 0, ut_line[32] at 8, ut_id[4] at 40, ut_user[32] at 44. Record size
// 384.
const (
	utmpRecordSize  = 384
	utmpTypeOffset  = 0
	utmpUserOffset  = 44
	utmpUserSize    = 32
	utmpUserProcess = 7
)

// countUsers returns the number of distinct logged-in users from the
// utmp database.
func countUsers() int {
	raw, err := os.ReadFile("/var/run/utmp")
	if err != nil {
		return 0
	}
	seen := make(map[string]bool)
	for off := 0; off+utmpRecordSize <= len(raw); off += utmpRecordSize {
		rec := raw[off : off+utmpRecordSize]
		if binary.LittleEndian.Uint16(rec[utmpTypeOffset:]) != utmpUserProcess {
			continue
		}
		if name := cstrField(rec[utmpUserOffset : utmpUserOffset+utmpUserSize]); name != "" {
			seen[name] = true
		}
	}
	return len(seen)
}
