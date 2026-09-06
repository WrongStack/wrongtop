// GPU polling shared across platforms: the cache cadence lives here and
// the probe is platform-specific (NVML everywhere except darwin).
package collector

import (
	"context"
	"time"
)

// probeGPUsFn indirects the platform probe so the cache/back-off logic
// can be exercised on machines without GPU reporting.
var probeGPUsFn = probeGPUs

// collectGPUs returns cached GPU readings, sampling through the platform
// hook at the GPU cadence. A failed probe (no NVIDIA driver, no adapter)
// backs off briefly so absent hardware costs nothing per tick.
func (c *Collector) collectGPUs(ctx context.Context, now time.Time) []GPU {
	c.gpuMu.Lock()
	defer c.gpuMu.Unlock()

	interval := c.gpuInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if c.gpuFailed {
		if now.Sub(c.lastGPU) < 30*time.Second {
			return c.cachedGPUs
		}
		c.gpuFailed = false // periodically re-probe: adapters can appear
	}
	if !c.lastGPU.IsZero() && now.Sub(c.lastGPU) < interval {
		return c.cachedGPUs
	}
	c.lastGPU = now
	gpus, err := probeGPUsFn(ctx)
	if err != nil || len(gpus) == 0 {
		c.gpuFailed = true
		c.cachedGPUs = nil
		return nil
	}
	c.cachedGPUs = gpus
	return gpus
}
