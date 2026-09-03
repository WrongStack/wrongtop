//go:build !linux

package collector

import "context"

// readBattery is not implemented on this platform.
func readBattery(context.Context) *Battery { return nil }
