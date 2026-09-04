//go:build !windows

package procs

import (
	"syscall"

	"github.com/shirou/gopsutil/v4/process"
)

// signalFor maps a picker name to an OS signal.
func signalFor(name string) (syscall.Signal, bool) {
	switch name {
	case "INT":
		return syscall.SIGINT, true
	case "HUP":
		return syscall.SIGHUP, true
	case "QUIT":
		return syscall.SIGQUIT, true
	case "STOP":
		return syscall.SIGSTOP, true
	case "CONT":
		return syscall.SIGCONT, true
	default:
		return 0, false
	}
}

// extraSignals reports whether the platform supports non-TERM/KILL
// signals.
func extraSignals() bool { return true }

// sendExtraSignal delivers a non-TERM/KILL signal; handled=false when
// the name is unknown.
func sendExtraSignal(p *process.Process, name string) (bool, error) {
	sig, ok := signalFor(name)
	if !ok {
		return false, nil
	}
	return true, p.SendSignal(sig)
}
