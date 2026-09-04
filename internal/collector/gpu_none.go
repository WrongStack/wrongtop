//go:build !linux && !windows

package collector

import (
	"context"
	"errors"
)

// probeGPUs is not implemented on this platform: there is no portable
// user-space GPU counter without the NVIDIA driver library (or, on
// macOS, without root powermetrics).
func probeGPUs(ctx context.Context) ([]GPU, error) {
	_ = ctx
	return nil, errors.New("gpu monitoring is not available on this platform")
}
