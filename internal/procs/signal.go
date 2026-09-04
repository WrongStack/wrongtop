package procs

import (
	"fmt"
	"os"

	"github.com/shirou/gopsutil/v4/process"
)

// Signal is a signal wrongtop can deliver to a process.
type Signal struct {
	Name string // TERM, KILL, INT, …
	Desc string // short label for the picker
}

// Signals lists the signals offered by the picker, in order. TERM and
// KILL mirror the classic terminate/force-kill shortcuts. On windows
// every entry except TERM and KILL is unavailable and the picker hides it.
var Signals = []Signal{
	{"TERM", "terminate"},
	{"KILL", "force kill"},
	{"INT", "interrupt"},
	{"HUP", "hangup"},
	{"QUIT", "quit + core"},
	{"STOP", "pause"},
	{"CONT", "resume"},
}

// SignalIndex returns the position of name in Signals, or -1.
func SignalIndex(name string) int {
	for i, s := range Signals {
		if s.Name == name {
			return i
		}
	}
	return -1
}

// AvailableSignals returns the signals this platform can deliver, in
// picker order.
func AvailableSignals() []Signal {
	if extraSignals() {
		return Signals
	}
	return Signals[:2] // TERM, KILL only (windows)
}

// SendSignal delivers the named signal to pid. TERM/KILL work on every
// platform (TerminateProcess on windows); the rest are unix-only.
func SendSignal(pid int32, name string) error {
	if int(pid) == os.Getpid() {
		return fmt.Errorf("refusing to signal wrongtop itself (pid %d)", pid)
	}
	p, err := process.NewProcess(pid)
	if err != nil {
		return fmt.Errorf("pid %d: %w", pid, err)
	}
	switch name {
	case "TERM":
		return p.Terminate()
	case "KILL":
		return p.Kill()
	}
	handled, err := sendExtraSignal(p, name)
	if err != nil {
		return err
	}
	if !handled {
		return fmt.Errorf("signal %s is not available on this platform", name)
	}
	return nil
}
