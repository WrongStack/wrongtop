//go:build !linux

package collector

// zramStats has nothing to report off linux; compressed swap devices are
// a kernel zram feature.
func zramStats() (total, used uint64) { return 0, 0 }
