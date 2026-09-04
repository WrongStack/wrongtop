//go:build windows

package collector

import (
	"context"

	"github.com/yusufpapurcu/wmi"
)

// platformTemps reports ACPI thermal zone temperatures via WMI. Many
// systems (and most VMs) do not implement MSAcpi_ThermalZoneTemperature,
// so failure simply yields no readings; gopsutil cannot help on windows.
func platformTemps(ctx context.Context) []Sensor {
	_ = ctx // wmi.Query has no context plumbing
	var zones []win32ThermalZone
	if err := wmi.QueryNamespace(
		"SELECT InstanceName, CurrentTemperature FROM MSAcpi_ThermalZoneTemperature",
		&zones, "root/wmi"); err != nil || len(zones) == 0 {
		return nil
	}
	var out []Sensor
	for _, z := range zones {
		// CurrentTemperature is tenths of a kelvin
		t := float64(z.CurrentTemperature)/10 - 273.15
		if t <= 0 || t > 120 { //nolint:mnd // sane physical bounds
			continue
		}
		out = append(out, Sensor{Name: z.InstanceName, TempC: t})
	}
	return out
}

type win32ThermalZone struct {
	InstanceName       string
	CurrentTemperature uint32
}
