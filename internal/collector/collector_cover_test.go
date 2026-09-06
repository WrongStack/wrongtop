package collector

import (
	"context"
	"errors"
	"math"
	"os"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// freePid returns a pid verified not to exist, so fallback code paths
// fail cleanly without touching a live process.
func freePid(t *testing.T) int32 {
	t.Helper()
	for pid := int32(2147483000); pid > 0; pid-- {
		if exists, err := process.PidExists(pid); err == nil && !exists {
			return pid
		}
	}
	t.Fatal("no free pid found")
	return 0
}

// stubProbes swaps the platform seams for the duration of a test.
func stubProbes(t *testing.T, sys func() (map[int32]procSys, error), usage func() (map[int32]procUsage, error), pids func(context.Context) ([]int32, error)) {
	t.Helper()
	os, ou, ol := readProcSysFn, readAllProcUsageFn, listPidsFn
	readProcSysFn, readAllProcUsageFn, listPidsFn = sys, usage, pids
	t.Cleanup(func() { readProcSysFn, readAllProcUsageFn, listPidsFn = os, ou, ol })
}

func TestCstrField(t *testing.T) {
	if got := cstrField([]byte{'a', 'b', 0, 'c'}); got != "ab" {
		t.Errorf("NUL-terminated: %q", got)
	}
	if got := cstrField([]byte("abc")); got != "abc" {
		t.Errorf("no NUL: %q", got)
	}
	if got := cstrField(nil); got != "" {
		t.Errorf("empty: %q", got)
	}
}

func TestConnRankAndSort(t *testing.T) {
	if connRank("CLOSE_WAIT") != 3 || connRank("TIME_WAIT") != 3 {
		t.Error("unknown states must sink to the tail")
	}
	cons := []Conn{
		{Local: "c:9", State: "CLOSE_WAIT"},
		{Local: "b:9", State: "LISTEN"},
		{Local: "a:9", State: "SYN_SENT"},
		{Local: "z:9", Remote: "x:1", State: "ESTABLISHED"},
		{Local: "a:1", Remote: "y:1", State: "ESTABLISHED"},
		{Local: "a:9", State: "SYN_RECV"},
	}
	sortConns(cons)
	got := ""
	for _, c := range cons {
		got += c.Local + " "
	}
	want := "a:1 z:9 a:9 a:9 b:9 c:9 " // established, SYN_*, LISTEN, tail; local/remote tie-break
	if got != want {
		t.Errorf("sortConns order:\n got %s want %s", got, want)
	}
}

func TestCmdline(t *testing.T) {
	if cl := Cmdline(context.Background(), int32(os.Getpid())); cl == "" {
		t.Error("own cmdline must resolve")
	}
	if cl := Cmdline(context.Background(), -1); cl != "" {
		t.Errorf("bogus pid must be empty, got %q", cl)
	}
	if cl := Cmdline(context.Background(), freePid(t)); cl != "" {
		t.Errorf("nonexistent pid must be empty, got %q", cl)
	}
}

func TestCollectGPUCacheAndBackoff(t *testing.T) {
	c := New(time.Second)
	now := time.Now()
	calls := 0

	probeGPUsFn = func(context.Context) ([]GPU, error) {
		calls++
		return nil, errors.New("no nvml")
	}
	t.Cleanup(func() { probeGPUsFn = probeGPUs })

	if gpus := c.collectGPUs(context.Background(), now); gpus != nil || !c.gpuFailed {
		t.Error("failed probe must back off")
	}
	c.collectGPUs(context.Background(), now.Add(time.Second))
	if calls != 1 {
		t.Errorf("back-off must suppress re-probing: %d calls", calls)
	}
	c.collectGPUs(context.Background(), now.Add(31*time.Second))
	if calls != 2 {
		t.Errorf("back-off must expire after 30s: %d calls", calls)
	}

	probeGPUsFn = func(context.Context) ([]GPU, error) {
		calls++
		return []GPU{{Index: 0, Name: "RTX"}}, nil
	}
	c.gpuFailed, c.lastGPU = false, time.Time{}
	gpus := c.collectGPUs(context.Background(), now)
	if len(gpus) != 1 || gpus[0].Name != "RTX" {
		t.Errorf("cached result: %+v", gpus)
	}
	if got := c.collectGPUs(context.Background(), now.Add(time.Millisecond)); len(got) != 1 || calls != 3 {
		t.Errorf("within the interval the cache must serve: calls=%d", calls)
	}

	probeGPUsFn = func(context.Context) ([]GPU, error) { calls++; return nil, nil }
	c.lastGPU = time.Time{}
	if gpus := c.collectGPUs(context.Background(), now); gpus != nil || !c.gpuFailed {
		t.Error("an empty adapter list must count as absent hardware")
	}

	zero := &Collector{} // zero value: interval defaults apply
	zero.collectGPUs(context.Background(), now.Add(time.Hour))
}

func TestCollectSensorsPicksHottestFour(t *testing.T) {
	orig := platformTempsFn
	platformTempsFn = func(context.Context) []Sensor {
		return []Sensor{{"a", 10}, {"b", 70}, {"c", 50}, {"d", 90}, {"e", 30}, {"f", 60}}
	}
	t.Cleanup(func() { platformTempsFn = orig })

	out := collectSensors(context.Background())
	if len(out) != 4 {
		t.Fatalf("cap: got %d sensors", len(out))
	}
	for i, want := range []float64{90, 70, 60, 50} {
		if out[i].TempC != want {
			t.Errorf("rank %d: %.0f, want %.0f", i, out[i].TempC, want)
		}
	}
}

func TestSensorRelevant(t *testing.T) {
	for _, name := range []string{"CPU", "coretemp", "k10temp", "acpitz", "soc_thermal", "Package id 0"} {
		if !sensorRelevant(name) {
			t.Errorf("%q should match", name)
		}
	}
	for _, name := range []string{"GPU", "battery", "PCH", ""} {
		if sensorRelevant(name) {
			t.Errorf("%q should not match", name)
		}
	}
}

func TestCollectProcsFallbackPath(t *testing.T) {
	self := int32(os.Getpid())
	ghost := freePid(t)
	stubProbes(t,
		func() (map[int32]procSys, error) { return nil, nil },   // no bulk identity
		func() (map[int32]procUsage, error) { return nil, nil }, // no fast path
		func(context.Context) ([]int32, error) { return []int32{self, ghost}, nil },
	)

	c := &Collector{}
	procs := c.collectProcs(context.Background(), 0, 1)
	if len(procs) != 2 {
		t.Fatalf("procs: %d", len(procs))
	}
	var mine Proc
	for _, p := range procs {
		if p.PID == self {
			mine = p
		} else if p.PID == ghost && (p.RSS != 0 || p.Name != "" || p.CPU != 0) {
			t.Errorf("ghost process must read as zeros: %+v", p)
		}
	}
	if mine.User == "" || mine.RSS == 0 || mine.Threads < 1 || mine.Name == "" {
		t.Errorf("self via gopsutil fallback incomplete: %+v", mine)
	}

	// a later poll diffs CPU against the remembered totals
	procs = c.collectProcs(context.Background(), 1, 1)
	for _, p := range procs {
		if p.PID == self && p.CPU < 0 {
			t.Errorf("cpu delta went negative: %+v", p)
		}
	}

	// listPids failing aborts the poll
	stubProbes(t,
		func() (map[int32]procSys, error) { return nil, nil },
		func() (map[int32]procUsage, error) { return nil, nil },
		func(context.Context) ([]int32, error) { return nil, errors.New("boom") },
	)
	if procs := c.collectProcs(context.Background(), 1, 1); procs != nil {
		t.Error("pid sweep failure must abort")
	}
}

func TestCollectProcsFastPath(t *testing.T) {
	self := int32(os.Getpid())
	stranger := int32(2) // in the pid table, task info unreadable
	stubProbes(t,
		func() (map[int32]procSys, error) {
			return map[int32]procSys{
				self:     {Name: "wrongtop.test", State: "R", UID: 0, PPID: 1, Nice: 10},
				stranger: {Name: "init", State: "S", UID: 0, PPID: 0},
			}, nil
		},
		func() (map[int32]procUsage, error) {
			return map[int32]procUsage{
				self: {Name: "wrongtop.test", RSS: 1 << 30, Threads: 4, CPUSecs: 10},
			}, nil
		},
		nil,
	)

	c := &Collector{}
	c.lastCPU = map[int32]float64{self: 8} // previous poll saw 8 cpu-seconds
	c.users = map[int32]string{}
	procs := c.collectProcs(context.Background(), 1, 4<<30)
	if len(procs) != 2 {
		t.Fatalf("procs: %d", len(procs))
	}
	for _, p := range procs {
		switch p.PID {
		case self:
			if p.State != "R" || p.Nice != 10 || p.User == "" ||
				p.RSS != 1<<30 || p.Mem != 25 || p.Threads != 4 || p.CPU != 200 {
				t.Errorf("fast-path sample wrong: %+v", p)
			}
		case stranger:
			if p.State != "S" || p.RSS != 0 || p.Threads != 0 || p.Name != "init" {
				t.Errorf("unreadable task info must read as zeros: %+v", p)
			}
		}
	}

	// churn: the reused map is rebuilt once most of the table exits
	stubProbes(t,
		func() (map[int32]procSys, error) {
			return map[int32]procSys{10: {State: "R", UID: 0}}, nil
		},
		func() (map[int32]procUsage, error) {
			return map[int32]procUsage{
				10: {CPUSecs: 1}, 11: {CPUSecs: 1}, 12: {CPUSecs: 1}, 13: {CPUSecs: 1},
			}, nil
		},
		nil,
	)
	c.collectProcs(context.Background(), 1, 4<<30) // primes lastCPU with pids 10..13
	stubProbes(t,
		func() (map[int32]procSys, error) {
			return map[int32]procSys{10: {State: "R", UID: 0}}, nil
		},
		func() (map[int32]procUsage, error) {
			return map[int32]procUsage{10: {CPUSecs: 2}}, nil
		},
		nil,
	)
	procs = c.collectProcs(context.Background(), 1, 4<<30)
	if len(procs) != 1 || len(c.lastCPU) != 1 || c.lastCPU[10] != 2 {
		t.Errorf("table shrink: procs=%d lastCPU=%v", len(procs), c.lastCPU)
	}

	// unknown uids render numerically
	if got := c.userFor(999999); got != "999999" {
		t.Errorf("unknown uid: %q", got)
	}
}

func TestCollectSlowAndHost(t *testing.T) {
	c := New(time.Hour) // slow path samples exactly once
	ctx := context.Background()

	s2 := c.Collect(ctx)
	if s2.Host.Uptime <= 0 {
		t.Errorf("uptime missing: %+v", s2.Host)
	}
	if s2.Host.Hostname == "" || s2.Host.Arch == "" {
		t.Errorf("host identity missing: %+v", s2.Host)
	}
	if s2.CPU.Percent < 0 || s2.Mem.Total == 0 {
		t.Errorf("cpu/mem sample missing: %+v %+v", s2.CPU, s2.Mem)
	}
	if len(s2.Disks) == 0 {
		t.Error("no mounts reported")
	}
	if s2.Host.Users < 0 {
		t.Error("user count negative")
	}
}

func TestRatesOverTwoPolls(t *testing.T) {
	c := New(time.Second)
	ctx := context.Background()
	c.Collect(ctx) // prime the counters
	time.Sleep(20 * time.Millisecond)
	snap := c.Collect(ctx)
	if len(snap.Nets) == 0 {
		t.Error("no network interfaces reported")
	}
	for _, n := range snap.Nets {
		if n.RxRate < 0 || n.TxRate < 0 || n.RxDrop < 0 || n.TxDrop < 0 {
			t.Errorf("negative rate on %s: %+v", n.Name, n)
		}
	}
	for _, d := range snap.DiskIOs {
		if d.BusyPercent < 0 || d.BusyPercent > 100 {
			t.Errorf("busy%% out of range on %s: %v", d.Name, d.BusyPercent)
		}
	}
	if math.IsNaN(snap.CPU.Percent) {
		t.Error("cpu percent is NaN")
	}
}
