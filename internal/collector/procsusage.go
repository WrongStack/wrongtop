package collector

// procUsage is one fast-path per-process sample from libproc (darwin).
// CPUSecs is cumulative user+system CPU time converted to seconds with
// the mach timebase — the same proc_taskinfo counters gopsutil's Times
// path reads, so both paths report identical units.
type procUsage struct {
	Name    string
	RSS     uint64
	Threads int32
	CPUSecs float64
}
