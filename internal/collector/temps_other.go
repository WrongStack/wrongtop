//go:build !darwin && !windows && !linux

package collector

import "context"

// platformTemps has no source on this platform: other kernels expose
// no temperature readings wrongtop can rely on. Linux reads hwmon
// (temps_linux.go).
func platformTemps(context.Context) []Sensor { return nil }
