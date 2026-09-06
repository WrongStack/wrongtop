//go:build darwin && cgo

package collector

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation

#include <libproc.h>
#include <mach/mach_time.h>
#include <sys/proc_info.h>

// wt_list_pids fills up to n pids and returns how many were written,
// or the required count when buf is NULL.
static int wt_list_pids(int *buf, int n) {
	return proc_listallpids(buf, n);
}

// wt_proc_usage samples one process from proc_taskinfo: RSS bytes,
// thread count, cumulative CPU seconds and its full name (up to
// 2*MAXCOMLEN; may be empty for protected processes). Returns 0 on
// success, -1 when task info is unavailable — the process exited, or it
// belongs to another user: unprivileged callers cannot read other users'
// task ports on macOS (ps and top carry a private entitlement for it).
static int wt_proc_usage(int pid, unsigned long long *rss,
	int *threads, double *cpu_secs, char *name, int namelen) {
	struct proc_taskinfo ti;
	if (proc_pidinfo(pid, PROC_PIDTASKINFO, 0, &ti, (int)sizeof(ti)) < (int)sizeof(ti)) {
		return -1;
	}
	*rss = ti.pti_resident_size;
	*threads = ti.pti_threadnum;

	// pti_total_user/system are mach timebase units (the same counters
	// gopsutil converts for its Times path); cache the timebase after
	// the first read.
	static mach_timebase_info_data_t tb;
	if (tb.denom == 0) {
		mach_timebase_info(&tb);
	}
	*cpu_secs = (double)(ti.pti_total_user + ti.pti_total_system)
		* tb.numer / tb.denom / 1e9;

	name[0] = 0;
	if (proc_name(pid, name, namelen) < 0) {
		name[0] = 0;
	}
	return 0;
}
*/
import "C"

import (
	"fmt"
)

// readAllProcUsage samples every readable process in one pass — a single
// proc_taskinfo call per pid replaces gopsutil's per-process dlopen +
// ProcPidInfo round trips. Coverage is partial by platform design: only
// the caller's own processes (all of them when running as root) expose
// task info, so a pid missing from the map has no readable metrics
// anywhere — gopsutil issues the same syscall and gets the same refusal.
func readAllProcUsage() (map[int32]procUsage, error) {
	n, err := C.wt_list_pids(nil, 0)
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("proc_listallpids count: %v", int(n))
	}
	pids := make([]C.int, n+1)
	got := C.wt_list_pids(&pids[0], C.int(len(pids)))
	if got <= 0 {
		return nil, fmt.Errorf("proc_listallpids: %d", int(got))
	}

	// out-params live outside the loop: taking their address for the C
	// call moves them to the heap, and per-pid copies would cost an
	// allocation each on every tick
	var (
		rss     C.ulonglong
		threads C.int
		cpuSecs C.double
		name    [64]C.char
	)
	out := make(map[int32]procUsage, got)
	for i := 0; i < int(got); i++ {
		pid := int32(pids[i])
		if rc := C.wt_proc_usage(C.int(pid), &rss, &threads, &cpuSecs, &name[0], C.int(len(name))); rc != 0 {
			continue // exited, or another user's process (task info unreadable)
		}
		out[pid] = procUsage{
			RSS:     uint64(rss),
			Threads: int32(threads),
			CPUSecs: float64(cpuSecs),
			Name:    C.GoString(&name[0]),
		}
	}
	return out, nil
}
