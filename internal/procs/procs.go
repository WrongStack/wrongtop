// Package procs implements process-table helpers (filter, sort) and
// process actions (terminate, force kill).
package procs

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/process"
	"github.com/wrongstack/wrongtop/internal/collector"
)

// SortKey selects the process table ordering.
type SortKey int

// Sort keys in cycle order.
const (
	SortCPU SortKey = iota
	SortMem
	SortPID
	SortName
	SortUser
)

// String returns the display name of a sort key.
func (k SortKey) String() string {
	switch k {
	case SortCPU:
		return "cpu"
	case SortMem:
		return "mem"
	case SortPID:
		return "pid"
	case SortName:
		return "name"
	case SortUser:
		return "user"
	default:
		return "?"
	}
}

// Next cycles to the following sort key.
func (k SortKey) Next() SortKey { return (k + 1) % 5 }

// Filter keeps processes whose name or user contains needle
// (case-insensitive) or, when needle is all digits, whose PID starts
// with it.
func Filter(procs []collector.Proc, needle string) []collector.Proc {
	needle = strings.TrimSpace(strings.ToLower(needle))
	if needle == "" {
		return procs
	}
	numeric := true
	for _, r := range needle {
		if r < '0' || r > '9' {
			numeric = false
			break
		}
	}

	out := make([]collector.Proc, 0, len(procs))
	for _, p := range procs {
		switch {
		case numeric:
			if strings.HasPrefix(strconv.Itoa(int(p.PID)), needle) {
				out = append(out, p)
			}
		case strings.Contains(strings.ToLower(p.Name), needle) ||
			strings.Contains(strings.ToLower(p.User), needle):
			out = append(out, p)
		}
	}
	return out
}

// Sort orders processes in place.
func Sort(procs []collector.Proc, key SortKey, desc bool) {
	slices.SortStableFunc(procs, func(a, b collector.Proc) int {
		var c int
		switch key {
		case SortCPU:
			c = cmp.Compare(a.CPU, b.CPU)
		case SortMem:
			c = cmp.Compare(a.Mem, b.Mem)
		case SortPID:
			c = cmp.Compare(a.PID, b.PID)
		case SortName:
			c = cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case SortUser:
			c = cmp.Compare(a.User, b.User)
		}
		if desc {
			c = -c
		}
		return c
	})
}

// Kill sends SIGTERM on unix; TerminateProcess on windows.
func Kill(pid int32) error {
	if int(pid) == os.Getpid() {
		return fmt.Errorf("refusing to kill wrongtop itself (pid %d)", pid)
	}
	p, err := process.NewProcess(pid)
	if err != nil {
		return fmt.Errorf("pid %d: %w", pid, err)
	}
	return p.Terminate()
}

// ForceKill sends SIGKILL on unix; TerminateProcess on windows.
func ForceKill(pid int32) error {
	if int(pid) == os.Getpid() {
		return fmt.Errorf("refusing to kill wrongtop itself (pid %d)", pid)
	}
	p, err := process.NewProcess(pid)
	if err != nil {
		return fmt.Errorf("pid %d: %w", pid, err)
	}
	return p.Kill()
}
