//go:build !darwin || !cgo

package collector

// readAllProcUsage has no libproc fast path here; collectProcs falls
// back to per-process gopsutil calls.
func readAllProcUsage() (map[int32]procUsage, error) { return nil, nil }
