package collector

import (
	"context"
	"math"
	"testing"
)

// The failure condition: a device or interface reinitialises (NIC
// bounce, driver reload, USB re-plug) and its kernel counters restart
// from zero, so the previous sample stored on the Collector now sits far
// above the current one. The test plants exactly that state by raising
// the stored previous counters by 2^62 (~4 exabytes of traffic — no
// host has ever counted that much), then drives the real Collect path:
// real gopsutil counters, real diffing, real rates. A uint64 subtraction
// would wrap to ~2^64 and yield rates around 10^21 B/s; the fix clamps
// the signed delta to 0.
const resetBump = uint64(1) << 62

// absurdRate bounds what any physical host can report per second. A
// wrapped delta produces ~10^21 B/s, so this bound cannot false-positive
// on legitimate traffic.
const absurdRate = 1e15

func isSaneRate(v float64) bool {
	return !math.IsInf(v, 0) && !math.IsNaN(v) && v >= 0 && v < absurdRate
}

func TestCollectNetCounterResetDoesNotWrap(t *testing.T) {
	c := &Collector{}
	ctx := context.Background()

	c.Collect(ctx) // warm-up: establishes pollClock and the real lastNet map

	c.mu.Lock()
	ifaces := len(c.lastNet)
	for name, st := range c.lastNet {
		st.BytesRecv += resetBump
		st.BytesSent += resetBump
		st.PacketsRecv += resetBump
		st.PacketsSent += resetBump
		c.lastNet[name] = st
	}
	c.mu.Unlock()
	if ifaces == 0 {
		t.Skip("gopsutil reports no network interfaces on this host")
	}

	snap := c.Collect(ctx)
	for _, ni := range snap.Nets {
		if !isSaneRate(ni.RxRate) || !isSaneRate(ni.TxRate) {
			t.Fatalf("interface %q reports absurd rate rx=%g tx=%g B/s after a simulated counter reset (uint64 delta wrap)", ni.Name, ni.RxRate, ni.TxRate)
		}
		if !isSaneRate(ni.RxRatePackets) || !isSaneRate(ni.TxRatePackets) {
			t.Fatalf("interface %q reports absurd packet rate rx=%g tx=%g pps after a simulated counter reset (uint64 delta wrap)", ni.Name, ni.RxRatePackets, ni.TxRatePackets)
		}
	}
}

func TestCollectDiskCounterResetDoesNotWrap(t *testing.T) {
	c := &Collector{}
	ctx := context.Background()

	c.Collect(ctx) // warm-up: establishes pollClock and the real lastDiskIO map

	c.mu.Lock()
	devices := len(c.lastDiskIO)
	for name, st := range c.lastDiskIO {
		st.ReadBytes += resetBump
		st.WriteBytes += resetBump
		st.ReadCount += resetBump
		st.WriteCount += resetBump
		c.lastDiskIO[name] = st
	}
	c.mu.Unlock()
	if devices == 0 {
		t.Skip("gopsutil reports no disk I/O devices on this host")
	}

	snap := c.Collect(ctx)
	for _, d := range snap.DiskIOs {
		if !isSaneRate(d.ReadBytes) || !isSaneRate(d.WriteBytes) ||
			!isSaneRate(d.ReadIOPS) || !isSaneRate(d.WriteIOPS) {
			t.Fatalf("device %q reports absurd I/O after a simulated counter reset (uint64 delta wrap): read=%g B/s write=%g B/s riops=%g wiops=%g", d.Name, d.ReadBytes, d.WriteBytes, d.ReadIOPS, d.WriteIOPS)
		}
	}
}
