//go:build darwin && !cgo

// Stubs for darwin builds compiled without cgo (CGO_ENABLED=0): the SMC
// and IOPowerSources are only reachable through IOKit, so temperatures,
// fans and battery stay empty in that configuration.
package collector

import "context"

func platformTemps(ctx context.Context) []Sensor { return nil }

func readFans() []Fan { return nil }

func readBattery(ctx context.Context) *Battery { return nil }
