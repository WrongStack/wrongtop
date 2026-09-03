//go:build !darwin

package collector

// procSys holds process fields that can be read in bulk on this platform.
type procSys struct {
	State string
	UID   int32
	PPID  int32
}

// readProcSys has no bulk implementation here; the collector falls back
// to per-process gopsutil calls, which are cheap (native /proc and WinAPI).
func readProcSys() (map[int32]procSys, error) {
	return nil, nil
}
