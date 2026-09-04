//go:build !darwin && !windows

package collector

import "context"

// platformTemps has no extra source on this platform: gopsutil covers
// hwmon readings on linux, other kernels expose nothing reliable.
func platformTemps(context.Context) []Sensor { return nil }
