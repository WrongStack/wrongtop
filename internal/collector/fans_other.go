//go:build !linux && !darwin

package collector

// Fan readings have no portable source on this platform (windows WMI
// Win32_Fan is effectively always empty; freebsd has no hwmon).
func readFans() []Fan { return nil }
