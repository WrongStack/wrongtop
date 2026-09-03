package collector

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
)

// hostIdentity is the slow-changing part of Host, fetched once.
type hostIdentity struct {
	Hostname string
	OS       string
	Platform string
	Kernel   string
	Arch     string
	Procs    int
}

// Collector polls system metrics on demand. It is safe for concurrent use.
type Collector struct {
	once sync.Once
	id   hostIdentity
}

// Collect gathers one snapshot. Sections that fail are left zeroed;
// collection never fails wholesale.
func (c *Collector) Collect(ctx context.Context) Snapshot {
	return Snapshot{
		Time: time.Now(),
		Host: c.collectHost(ctx),
		CPU:  collectCPU(ctx),
		Mem:  collectMem(ctx),
	}
}

func (c *Collector) collectHost(ctx context.Context) Host {
	c.once.Do(func() {
		info, err := host.InfoWithContext(ctx)
		if err != nil {
			return
		}
		c.id = hostIdentity{
			Hostname: info.Hostname,
			OS:       info.OS,
			Platform: strings.TrimSpace(info.Platform + " " + info.PlatformVersion),
			Kernel:   info.KernelVersion,
			Arch:     info.KernelArch,
			Procs:    int(info.Procs),
		}
	})

	h := Host{
		Hostname: c.id.Hostname,
		OS:       c.id.OS,
		Platform: c.id.Platform,
		Kernel:   c.id.Kernel,
		Arch:     c.id.Arch,
		Procs:    c.id.Procs,
	}
	if uptime, err := host.UptimeWithContext(ctx); err == nil {
		h.Uptime = time.Duration(uptime) * time.Second
	}
	if avg, err := load.AvgWithContext(ctx); err == nil {
		h.Load = [3]float64{avg.Load1, avg.Load5, avg.Load15}
	}
	return h
}

// collectCPU reads non-blocking CPU percentages: gopsutil diffs against
// the previous call, which lines up with our refresh cadence.
func collectCPU(ctx context.Context) CPU {
	var c CPU
	if pct, err := cpu.PercentWithContext(ctx, 0, false); err == nil && len(pct) == 1 {
		c.Percent = pct[0]
	}
	if pct, err := cpu.PercentWithContext(ctx, 0, true); err == nil {
		c.Cores = pct
	}
	return c
}

func collectMem(ctx context.Context) Mem {
	var m Mem
	if v, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		m.Total = v.Total
		m.Used = v.Used
		m.Available = v.Available
		m.Percent = v.UsedPercent
	}
	if s, err := mem.SwapMemoryWithContext(ctx); err == nil {
		m.SwapTotal = s.Total
		m.SwapUsed = s.Used
		m.SwapPercent = s.UsedPercent
	}
	return m
}
