//go:build darwin

package collector

import "context"

// gopsutilSensors is compiled out on darwin: gopsutil's sensor probe
// costs ~50ms per call there and yields nothing — the AppleSMC reader
// (platformTemps) is both faster and better.
func gopsutilSensors(context.Context) []Sensor { return nil }
