//go:build !linux && !darwin

package collector

// countUsers has no implementation on this platform; windows session
// enumeration needs WMI and stays out of scope for now.
func countUsers() int { return 0 }
