package collector

import (
	"context"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
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

	mu       sync.Mutex
	lastCPU  map[int32]float64   // pid -> user+system CPU seconds
	lastPoll time.Time           // wall clock of previous process poll
	users    map[int32]string    // uid -> username cache
}

// Collect gathers one snapshot. Sections that fail are left zeroed;
// collection never fails wholesale.
func (c *Collector) Collect(ctx context.Context) Snapshot {
	return Snapshot{
		Time:  time.Now(),
		Host:  c.collectHost(ctx),
		CPU:   collectCPU(ctx),
		Mem:   collectMem(ctx),
		Procs: c.collectProcs(ctx),
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

// collectProcs lists all processes. Per-process CPU is diffed against the
// previous poll, matching the refresh cadence; the first poll reports 0.
func (c *Collector) collectProcs(ctx context.Context) []Proc {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.lastCPU == nil {
		c.lastCPU = make(map[int32]float64)
		c.users = make(map[int32]string)
	}

	now := time.Now()
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil
	}

	// Bulk state/uid/ppid where available (darwin); nil means fall back
	// to per-process calls below.
	sys, _ := readProcSys()

	next := make(map[int32]float64, len(procs))
	elapsed := now.Sub(c.lastPoll).Seconds()
	out := make([]Proc, 0, len(procs))
	for _, p := range procs {
		var pr Proc
		pr.PID = p.Pid
		if name, err := p.NameWithContext(ctx); err == nil {
			pr.Name = name
		}
		if t, err := p.TimesWithContext(ctx); err == nil {
			total := t.User + t.System
			if prev, ok := c.lastCPU[p.Pid]; ok && elapsed > 0 {
				pr.CPU = max(0, (total-prev)/elapsed*100)
			}
			next[p.Pid] = total
		}
		if mi, err := p.MemoryInfoWithContext(ctx); err == nil {
			pr.RSS = mi.RSS
		}
		if nt, err := p.NumThreadsWithContext(ctx); err == nil {
			pr.Threads = nt
		}
		if s, ok := sys[p.Pid]; ok {
			pr.State = s.State
			pr.PPID = s.PPID
			pr.User = c.userFor(s.UID)
		} else {
			if pp, err := p.PpidWithContext(ctx); err == nil {
				pr.PPID = pp
			}
			if st, err := p.StatusWithContext(ctx); err == nil && len(st) > 0 {
				pr.State = abbrevState(st[0])
			}
			if u, err := p.UsernameWithContext(ctx); err == nil {
				pr.User = u
			}
		}
		out = append(out, pr)
	}

	c.lastCPU = next
	c.lastPoll = now
	return out
}

// userFor resolves a uid to a username with caching; unknown uids render
// as their numeric value.
func (c *Collector) userFor(uid int32) string {
	if name, ok := c.users[uid]; ok {
		return name
	}
	name := strconv.FormatUint(uint64(uid), 10)
	if u, err := user.LookupId(name); err == nil && u.Username != "" {
		name = u.Username
	}
	c.users[uid] = name
	return name
}

// abbrevState reduces gopsutil status strings to a single htop-style letter.
func abbrevState(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "running", "r":
		return "R"
	case "sleeping", "s":
		return "S"
	case "zombie", "z":
		return "Z"
	case "stopped", "traced", "t":
		return "T"
	case "idle", "i":
		return "I"
	case "waiting", "w":
		return "W"
	case "blocked", "b":
		return "B"
	case "locked", "l":
		return "L"
	default:
		return strings.ToUpper(s[:1])
	}
}
