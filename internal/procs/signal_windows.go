//go:build windows

package procs

import "github.com/shirou/gopsutil/v4/process"

// extraSignals reports whether the platform supports non-TERM/KILL
// signals.
func extraSignals() bool { return false }

// sendExtraSignal has nothing to send on windows: only TERM and KILL
// exist there, and both are handled directly as TerminateProcess.
func sendExtraSignal(*process.Process, string) (bool, error) { return false, nil }
