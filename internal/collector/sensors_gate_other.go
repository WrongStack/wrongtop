//go:build !darwin

package collector

import (
	"context"

	"github.com/shirou/gopsutil/v4/sensors"
)

// gopsutilSensors collects CPU-relevant temperatures via gopsutil — the
// first choice everywhere but darwin, where hwmon (linux) and the ACPI
// path are cheap enough to try first.
func gopsutilSensors(ctx context.Context) []Sensor {
	temps, err := sensors.TemperaturesWithContext(ctx)
	if err != nil && len(temps) == 0 {
		return nil
	}
	var out []Sensor
	for _, s := range temps {
		if s.Temperature <= 0 {
			continue
		}
		if !sensorRelevant(s.SensorKey) {
			continue
		}
		out = append(out, Sensor{Name: s.SensorKey, TempC: s.Temperature})
	}
	return out
}
