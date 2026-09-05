package collector

// procUsage is one fast-path per-process sample from libproc (darwin).
// Deliberately unit-free: CPU time stays on gopsutil's Times path
// because rusage_info time fields are undocumented scheduler ticks
// (24,000,000 per CPU-second on Apple Silicon, measured).
type procUsage struct {
	Name    string
	RSS     uint64
	Threads int32
}
