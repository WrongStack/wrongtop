package collector

import (
	"context"
	"os/user"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
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
// The zero value is usable; use New to size the slow-metric cadence.
type Collector struct {
	once sync.Once
	id   hostIdentity

	// slowInterval throttles metrics that are expensive or slow-changing
	// (temperatures, frequency, battery) to one sample per interval.
	slowInterval time.Duration

	// gpuInterval throttles nvidia-smi polling, which is a process
	// spawn per sample.
	gpuInterval time.Duration

	mu         sync.Mutex
	lastCPU    map[int32]float64 // pid -> user+system CPU seconds
	lastDiskIO map[string]disk.IOCountersStat
	lastNet    map[string]net.IOCountersStat
	users      map[int32]string // uid -> username cache
	pollClock  time.Time        // previous Collect wall clock

	slowMu        sync.Mutex
	lastSlow      time.Time
	cachedFreq    float64
	cachedSensors []Sensor
	cachedFans    []Fan
	cachedBattery *Battery
	cachedUsers   int // logged-in sessions; parsed from utmpx on the slow path

	connMu       sync.Mutex
	connInterval time.Duration
	lastConn     time.Time
	cachedConns  []Conn

	gpuMu      sync.Mutex
	lastGPU    time.Time
	cachedGPUs []GPU
	gpuFailed  bool // nvidia-smi missing; back off before retrying
}

// New returns a collector whose slow metrics are sampled at most once per
// slowInterval (floored at 5s, since they are costly on some platforms).
func New(refresh time.Duration) *Collector {
	return &Collector{
		slowInterval: max(5*time.Second, refresh*5),
		gpuInterval:  max(2*time.Second, refresh*2),
		// the socket table costs a sysctl-level sweep (~40ms on darwin);
		// five seconds is plenty fresh for a human-readable table
		connInterval: max(5*time.Second, refresh*5),
	}
}

// Collect gathers one snapshot. Sections that fail are left zeroed;
// collection never fails wholesale. Rate-based metrics are deltas over
// the interval between Collect calls.
func (c *Collector) Collect(ctx context.Context) Snapshot {
	c.mu.Lock()
	now := time.Now()
	elapsed := now.Sub(c.pollClock).Seconds()
	c.pollClock = now
	c.mu.Unlock()
	if elapsed <= 0 || elapsed > 30 { // first run or resumed suspension
		elapsed = 0
	}

	freq, sensors, fans, battery := c.collectSlow(ctx, now)
	cpu := collectCPU(ctx)
	cpu.FreqMHz = freq
	mem := collectMem(ctx)
	mem.ZramTotal, mem.ZramUsed = zramStats()

	return Snapshot{
		Time:    now,
		Host:    c.collectHost(ctx),
		CPU:     cpu,
		Mem:     mem,
		Sensors: sensors,
		Fans:    fans,
		Battery: battery,
		GPUs:    c.collectGPUs(ctx, now),
		Procs:   c.collectProcs(ctx, elapsed, mem.Total),
		Disks:   c.collectDisks(ctx),
		DiskIOs: c.collectDiskIO(ctx, elapsed),
		Nets:    c.collectNet(ctx, elapsed),
		Conns:   c.collectConns(ctx, now),
	}
}

// collectSlow returns frequency, temperatures, fans and battery state,
// reading them from cache unless the slow interval has elapsed.
func (c *Collector) collectSlow(ctx context.Context, now time.Time) (float64, []Sensor, []Fan, *Battery) {
	c.slowMu.Lock()
	defer c.slowMu.Unlock()

	interval := c.slowInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if c.lastSlow.IsZero() || now.Sub(c.lastSlow) >= interval {
		c.lastSlow = now
		c.cachedFreq = collectFreq(ctx)
		c.cachedSensors = collectSensors(ctx)
		c.cachedFans = readFans()
		c.cachedBattery = readBattery(ctx)
		c.cachedUsers = countUsers()
	}
	return c.cachedFreq, c.cachedSensors, c.cachedFans, c.cachedBattery
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
	h.Users = c.cachedUsers // utmpx parse rides the slow cadence
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

// platformTempsFn indirects the platform sensor hook so the shared
// selection logic can be exercised on any machine.
var platformTempsFn = platformTemps

// sensorHints match CPU-relevant sensor names across vendors (coretemp,
// k10temp, cpu_thermal, packageid, acpi, soc dts, ...).
var sensorHints = []string{"cpu", "core", "thermal", "package", "k10temp", "acpi", "soc"}

// sensorRelevant reports whether a sensor name matches a CPU hint.
func sensorRelevant(name string) bool {
	name = strings.ToLower(name)
	for _, hint := range sensorHints {
		if strings.Contains(name, hint) {
			return true
		}
	}
	return false
}

// collectSensors returns CPU-relevant temperature readings, hottest
// first, capped at a handful. Readings of 0°C (missing data) are dropped;
// an empty result means the platform exposes nothing useful. When
// gopsutil yields nothing the platform hook gets a chance (AppleSMC on
// darwin, ACPI thermal zones on windows).
func collectSensors(ctx context.Context) []Sensor {
	out := gopsutilSensors(ctx)
	if len(out) == 0 {
		out = platformTempsFn(ctx)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TempC > out[j].TempC })
	return out[:min(len(out), 4)]
}

// collectFreq returns the average current CPU clock in MHz, or 0 when the
// platform does not expose per-core frequencies.
func collectFreq(ctx context.Context) float64 {
	infos, err := cpu.InfoWithContext(ctx)
	if err != nil {
		return 0
	}
	var sum float64
	var n int
	for _, in := range infos {
		if in.Mhz > 0 {
			sum += in.Mhz
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
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
// previous poll; the first poll reports 0. Mem is the share of physical
// memory in use, derived locally from RSS and the host total.
// Bulk probes and the pid source are indirected for tests: the darwin
// fast paths always succeed here, so the gopsutil fallback branches —
// the only path on linux and windows — are exercised by stubbing.
var (
	readProcSysFn      = readProcSys
	readAllProcUsageFn = readAllProcUsage
	listPidsFn         = process.PidsWithContext
)

func (c *Collector) collectProcs(ctx context.Context, elapsed float64, memTotal uint64) []Proc {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.lastCPU == nil {
		c.lastCPU = make(map[int32]float64)
		c.users = make(map[int32]string)
	}

	// Bulk identity (darwin) and libproc usage sampling (darwin+cgo)
	// replace the per-process gopsutil calls where available; nil maps
	// mean "fall back to per-process gopsutil" below.
	sys, _ := readProcSysFn()
	usage, _ := readAllProcUsageFn()

	// Pids + bare Process structs — deliberately NOT
	// process.ProcessesWithContext: it wraps every pid in
	// NewProcessWithContext, which eagerly probes CreateTime per process
	// (a proc_pidinfo sweep that alone was ~47% of Collect) for a field
	// wrongtop never reads. The bulk reads already carry the full pid
	// table, so a separate PidsWithContext sweep only runs on fallback
	// platforms.
	var pids []int32
	if sys != nil {
		pids = make([]int32, 0, len(sys))
		for pid := range sys {
			pids = append(pids, pid)
		}
		slices.Sort(pids)
	} else if list, err := listPidsFn(ctx); err == nil {
		pids = list
	} else {
		return nil
	}

	// lastCPU is reused in place — no map allocation on a stable process
	// table. It is rebuilt only when churn leaves it meaningfully larger
	// than the live table, so exited-pid capacity is not kept forever.
	next := c.lastCPU
	if next == nil {
		next = make(map[int32]float64, len(pids))
	}
	out := make([]Proc, 0, len(pids))
	var p process.Process // gopsutil handle, reset per pid: only fallback paths touch it
	for _, pid := range pids {
		p = process.Process{Pid: pid}
		var pr Proc
		pr.PID = pid
		s, hasSys := sys[pid]
		if hasSys {
			pr.State = s.State
			pr.PPID = s.PPID
			pr.Nice = s.Nice
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
			if n, err := p.NiceWithContext(ctx); err == nil {
				pr.Nice = int8(n)
			}
		}

		if u, has := usage[pid]; has {
			// libproc fast path: name, RSS, threads, and CPU seconds
			// straight from proc_taskinfo — the same counters gopsutil's
			// Times path converts, minus a per-process dlopen + ProcPidInfo
			// round trip
			pr.RSS = u.RSS
			pr.Mem = memPercent(u.RSS, memTotal)
			pr.Threads = u.Threads
			if pr.Name == "" {
				pr.Name = u.Name
			}
			if prev, ok := c.lastCPU[pid]; ok && elapsed > 0 {
				pr.CPU = max(0, (u.CPUSecs-prev)/elapsed*100)
			}
			next[pid] = u.CPUSecs
		} else if usage == nil {
			// gopsutil fallbacks for the fast-path metrics. A pid the fast
			// path rejected is deliberately NOT retried here: task info is
			// unreadable for it, and gopsutil would burn the same syscall
			// to learn the same thing.
			if t, err := p.TimesWithContext(ctx); err == nil {
				total := t.User + t.System
				if prev, ok := c.lastCPU[pid]; ok && elapsed > 0 {
					pr.CPU = max(0, (total-prev)/elapsed*100)
				}
				next[pid] = total
			}
			if mi, err := p.MemoryInfoWithContext(ctx); err == nil {
				pr.RSS = mi.RSS
				pr.Mem = memPercent(mi.RSS, memTotal)
			}
			if nt, err := p.NumThreadsWithContext(ctx); err == nil {
				pr.Threads = nt
			}
		}

		// name priority: libproc proc_name > kinfo P_Comm > gopsutil
		if pr.Name == "" && hasSys {
			pr.Name = s.Name
		}
		if pr.Name == "" {
			if name, err := p.NameWithContext(ctx); err == nil {
				pr.Name = name
			}
		}
		out = append(out, pr)
	}

	if len(next) > len(pids)+len(pids)/4 {
		fresh := make(map[int32]float64, len(pids))
		for _, pid := range pids {
			if v, ok := next[pid]; ok {
				fresh[pid] = v
			}
		}
		next = fresh
	}
	c.lastCPU = next
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

// pseudoFSTypes are virtual filesystems that carry no useful usage data.
var pseudoFSTypes = map[string]bool{
	"autofs":    true,
	"devfs":     true,
	"devtmpfs":  true,
	"procfs":    true,
	"linprocfs": true,
	"fdescfs":   true,
	"efivarfs":  true,
	"swap":      true,
}

func (c *Collector) collectDisks(ctx context.Context) []Disk {
	parts, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return nil
	}
	seen := make(map[string]bool, len(parts))
	out := make([]Disk, 0, len(parts))
	for _, p := range parts {
		if pseudoFSTypes[p.Fstype] || seen[p.Mountpoint] {
			continue
		}
		u, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil || u.Total == 0 {
			continue
		}
		seen[p.Mountpoint] = true
		out = append(out, Disk{
			Device:     p.Device,
			Mountpoint: p.Mountpoint,
			FSType:     p.Fstype,
			Total:      u.Total,
			Used:       u.Used,
			Free:       u.Free,
			Percent:    u.UsedPercent,
		})
	}
	return out
}

// collectDiskIO reports per-device read/write rates from counter deltas.
func (c *Collector) collectDiskIO(ctx context.Context, elapsed float64) []DiskIO {
	counters, err := disk.IOCountersWithContext(ctx)
	if err != nil {
		return nil
	}

	c.mu.Lock()
	prev := c.lastDiskIO
	c.lastDiskIO = counters
	c.mu.Unlock()

	if elapsed <= 0 || prev == nil {
		return nil
	}
	out := make([]DiskIO, 0, len(counters))
	for name, cur := range counters {
		p, ok := prev[name]
		if !ok {
			continue
		}
		busyMs := max(0, int64(cur.IoTime)-int64(p.IoTime))
		// kernel counters restart from zero when a device reinitialises;
		// subtract as int64 so the reset clamps to 0 instead of wrapping
		// into a ~2^64 spike
		d := DiskIO{
			Name:        name,
			ReadBytes:   max(0, float64(int64(cur.ReadBytes)-int64(p.ReadBytes))/elapsed),
			WriteBytes:  max(0, float64(int64(cur.WriteBytes)-int64(p.WriteBytes))/elapsed),
			ReadIOPS:    max(0, float64(int64(cur.ReadCount)-int64(p.ReadCount))/elapsed),
			WriteIOPS:   max(0, float64(int64(cur.WriteCount)-int64(p.WriteCount))/elapsed),
			BusyPercent: min(100, float64(busyMs)/1000/elapsed*100),
		}
		if d.ReadBytes > 0 || d.WriteBytes > 0 || d.ReadIOPS > 0 || d.WriteIOPS > 0 {
			out = append(out, d)
		}
	}
	return out
}

// collectNet reports per-interface traffic rates from counter deltas.
func (c *Collector) collectNet(ctx context.Context, elapsed float64) []NetIface {
	counters, err := net.IOCountersWithContext(ctx, true)
	if err != nil {
		return nil
	}

	c.mu.Lock()
	prev := c.lastNet
	c.lastNet = make(map[string]net.IOCountersStat, len(counters))
	for _, n := range counters {
		c.lastNet[n.Name] = n
	}
	c.mu.Unlock()

	if elapsed <= 0 || prev == nil {
		return nil
	}
	out := make([]NetIface, 0, len(counters))
	for _, cur := range counters {
		p, ok := prev[cur.Name]
		if !ok {
			continue
		}
		// kernel counters restart from zero when an interface bounces;
		// subtract as int64 so the reset clamps to 0 instead of wrapping
		// into a ~2^64 spike
		n := NetIface{
			Name:          cur.Name,
			RxRate:        max(0, float64(int64(cur.BytesRecv)-int64(p.BytesRecv))/elapsed),
			TxRate:        max(0, float64(int64(cur.BytesSent)-int64(p.BytesSent))/elapsed),
			RxRatePackets: max(0, float64(int64(cur.PacketsRecv)-int64(p.PacketsRecv))/elapsed),
			TxRatePackets: max(0, float64(int64(cur.PacketsSent)-int64(p.PacketsSent))/elapsed),
			RxDrop:        max(0, float64(int64(cur.Dropin)-int64(p.Dropin))/elapsed),
			TxDrop:        max(0, float64(int64(cur.Dropout)-int64(p.Dropout))/elapsed),
			RxTotal:       cur.BytesRecv,
			TxTotal:       cur.BytesSent,
		}
		if n.RxTotal > 0 || n.TxTotal > 0 || n.RxRate > 0 || n.TxRate > 0 {
			out = append(out, n)
		}
	}
	return out
}

// connCap bounds the connections table pushed into snapshots; the raw
// socket table can run to thousands of rows on busy servers.
const connCap = 300

// connRank orders connections for display: live traffic first, then
// listeners, then the tail of half-open and closing states. Unknown
// states sink to the tail with the rest.
func connRank(state string) int {
	switch state {
	case "ESTABLISHED":
		return 0
	case "SYN_SENT", "SYN_RECV":
		return 1
	case "LISTEN":
		return 2
	default:
		return 3
	}
}

// collectConns returns the TCP table, cached on its own cadence: the
// socket table costs a sysctl (~40ms on darwin), fine every couple of
// seconds, too dear for every refresh tick. PIDs ride along when the
// platform resolves them without privileges (darwin does; linux
// deliberately doesn't — the /proc scan would dominate collection).
func (c *Collector) collectConns(ctx context.Context, now time.Time) []Conn {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	interval := c.connInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if c.lastConn.IsZero() || now.Sub(c.lastConn) >= interval {
		c.lastConn = now
		c.cachedConns = sampleConns(ctx)
	}
	return c.cachedConns
}

func sampleConns(ctx context.Context) []Conn {
	cons, err := net.ConnectionsWithContext(ctx, "tcp")
	if err != nil {
		return nil
	}
	out := make([]Conn, 0, len(cons))
	for _, cn := range cons {
		out = append(out, Conn{
			Local:  connAddr(cn.Laddr.IP, cn.Laddr.Port),
			Remote: connAddr(cn.Raddr.IP, cn.Raddr.Port),
			State:  cn.Status,
			PID:    cn.Pid,
		})
	}
	sortConns(out)
	return out[:min(len(out), connCap)]
}

// sortConns orders the table for display: live traffic first, then
// listeners, then the tail, stable within each group.
func sortConns(out []Conn) {
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := connRank(out[i].State), connRank(out[j].State)
		if ri != rj {
			return ri < rj
		}
		if out[i].Local != out[j].Local {
			return out[i].Local < out[j].Local
		}
		return out[i].Remote < out[j].Remote
	})
}

// connAddr formats an ip:port pair, bracketing IPv6; a zero port yields
// the wildcard "*".
func connAddr(ip string, port uint32) string {
	if ip == "" {
		ip = "*"
	} else if strings.Contains(ip, ":") {
		ip = "[" + ip + "]"
	}
	if port == 0 {
		return ip + ":*"
	}
	return ip + ":" + strconv.FormatUint(uint64(port), 10)
}

// memPercent converts an RSS sample into a percent of physical memory.
func memPercent(rss, memTotal uint64) float64 {
	if rss == 0 || memTotal == 0 {
		return 0
	}
	return float64(rss) / float64(memTotal) * 100
}

// cstrField cuts a NUL-padded fixed-size field from a binary record.
func cstrField(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// Cmdline returns the full command line of one process, for the
// process-detail view. It is deliberately on-demand: gopsutil spawns a
// `ps` subprocess per call on darwin, so this must never run in the
// collection loop.
func Cmdline(ctx context.Context, pid int32) string {
	p, err := process.NewProcess(pid)
	if err != nil {
		return ""
	}
	cl, err := p.CmdlineWithContext(ctx)
	if err != nil {
		return ""
	}
	return cl
}

// abbrevState reduces gopsutil status strings to a single htop-style letter.
func abbrevState(s string) string {
	state := strings.ToLower(strings.TrimSpace(s))
	switch state {
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
	case "":
		// gopsutil maps unknown ps state chars to "" (its UnknownState
		// constant) and returns it with a nil error; render as unknown
		return "?"
	default:
		return strings.ToUpper(state[:1])
	}
}
