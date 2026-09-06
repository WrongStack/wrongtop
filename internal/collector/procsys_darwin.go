//go:build darwin

package collector

import "golang.org/x/sys/unix"

// procSys holds process fields that can be read in bulk on this platform.
type procSys struct {
	Name  string
	State string
	UID   int32
	PPID  int32
	Nice  int8
}

// readProcSys returns name/state/uid/ppid/nice for every process in one
// sysctl call. gopsutil's StatusWithContext would otherwise spawn one
// `ps` subprocess per process on darwin.
func readProcSys() (map[int32]procSys, error) {
	return readProcSysName("kern.proc.all")
}

func readProcSysName(sysctlName string) (map[int32]procSys, error) {
	kprocs, err := unix.SysctlKinfoProcSlice(sysctlName)
	if err != nil {
		return nil, err
	}
	out := make(map[int32]procSys, len(kprocs))
	var name [len(unix.KinfoProc{}.Proc.P_comm)]byte // stack scratch: one string alloc per proc, not two
	for i := range kprocs {
		k := &kprocs[i]
		n := 0
		for _, c := range k.Proc.P_comm {
			if c == 0 {
				break
			}
			name[n] = byte(c)
			n++
		}
		out[k.Proc.P_pid] = procSys{
			Name:  string(name[:n]),
			State: kinfoState(k.Proc.P_stat),
			UID:   int32(k.Eproc.Ucred.Uid),
			PPID:  k.Eproc.Ppid,
			Nice:  k.Proc.P_nice,
		}
	}
	return out, nil
}

// kinfoState maps macOS process states (sys/proc.h) to htop-style letters.
func kinfoState(s int8) string {
	switch s {
	case 1: // SIDL
		return "I"
	case 2: // SRUN
		return "R"
	case 3: // SSLEEP
		return "S"
	case 4: // SSTOP
		return "T"
	case 5: // SZOMB
		return "Z"
	default:
		return "?"
	}
}
