//go:build windows

package collector

import (
	"context"

	"github.com/yusufpapurcu/wmi"
)

// readBattery reports the system battery via WMI. A nil result means the
// machine has no battery (desktops) or WMI refused the query.
func readBattery(ctx context.Context) *Battery {
	_ = ctx // wmi.Query has no context plumbing
	var bats []win32Battery
	if err := wmi.Query("SELECT EstimatedChargeRemaining, BatteryStatus FROM Win32_Battery", &bats); err != nil || len(bats) == 0 {
		return nil
	}
	b := &bats[0]
	return &Battery{Percent: float64(b.EstimatedChargeRemaining), Charging: b.charging()}
}

type win32Battery struct {
	EstimatedChargeRemaining uint16
	BatteryStatus            uint16
}

// charging interprets Win32_Battery.BatteryStatus: 6-9 are the explicit
// charging states, 3 is "fully charged" (still on AC).
func (b win32Battery) charging() bool {
	return (b.BatteryStatus >= 6 && b.BatteryStatus <= 9) || b.BatteryStatus == 3
}
