package collector

import (
	"context"
	"os"
	"testing"
	"time"
)

func BenchmarkCollectFull(b *testing.B) {
	c := New(time.Second)
	ctx := context.Background()
	c.Collect(ctx) // warm up once (rates need a previous sample)
	b.ResetTimer()
	for range b.N {
		c.Collect(ctx)
	}
}

func BenchmarkCollectSlowPath(b *testing.B) {
	c := New(time.Second)
	ctx := context.Background()
	// force the slow-metric path (sensors, fans, battery) every iteration
	for range b.N {
		c.slowMu.Lock()
		c.lastSlow = time.Time{}
		c.slowMu.Unlock()
		c.Collect(ctx)
	}
}

func BenchmarkCollectFreq(b *testing.B) {
	ctx := context.Background()
	for range b.N {
		collectFreq(ctx)
	}
}

func BenchmarkGopsutilSensors(b *testing.B) {
	ctx := context.Background()
	for range b.N {
		gopsutilSensors(ctx)
	}
}

func BenchmarkReadFansSMC(b *testing.B) {
	for range b.N {
		readFans()
	}
}

func BenchmarkReadTempsSMC(b *testing.B) {
	for range b.N {
		platformTemps(context.Background())
	}
}

func BenchmarkReadBattery(b *testing.B) {
	ctx := context.Background()
	for range b.N {
		readBattery(ctx)
	}
}

// TestLiveProcUsageFields validates the fast path's unit-free metrics
// against our own process. CPU time is deliberately NOT checked here:
// rusage_info time fields are undocumented scheduler ticks (measured
// 24,000,000 per CPU-second on Apple Silicon), so CPU stays on the
// gopsutil Times path which uses known units.
func TestLiveProcUsageFields(t *testing.T) {
	usage, err := readAllProcUsage()
	if err != nil {
		t.Skipf("fast path unavailable: %v", err)
	}
	self, ok := usage[int32(os.Getpid())]
	if !ok {
		t.Skip("self not found in the pid list")
	}
	if self.RSS < 1<<20 { // a running Go test is at least a few MiB
		t.Errorf("self RSS = %d bytes — suspiciously small", self.RSS)
	}
	if self.Threads < 1 {
		t.Errorf("self threads = %d, want >= 1", self.Threads)
	}
	t.Logf("self: name=%q rss=%dMiB threads=%d", self.Name, self.RSS>>20, self.Threads)
}
