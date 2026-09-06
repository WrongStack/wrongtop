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
// against our own process.
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

// TestLiveProcCPUSeconds proves the fast path's CPU seconds carry real
// units: a busy loop burns roughly one CPU-second per wall second, and
// the proc_taskinfo delta must agree. Guards against a macOS revision
// changing the counter's timebase out from under the conversion.
func TestLiveProcCPUSeconds(t *testing.T) {
	const burn = 150 * time.Millisecond
	before, err := readAllProcUsage()
	if err != nil {
		t.Skipf("fast path unavailable: %v", err)
	}
	self := int32(os.Getpid())
	b0, ok := before[self]
	if !ok {
		t.Skip("self not found in the pid list")
	}

	start := time.Now()
	for time.Since(start) < burn { // single-goroutine busy loop: cpu ≈ wall
	}
	elapsed := time.Since(start)

	after, err := readAllProcUsage()
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	a1, ok := after[self]
	if !ok {
		t.Fatal("self exited the pid list mid-test")
	}

	burned := a1.CPUSecs - b0.CPUSecs
	if burned < 0.5*elapsed.Seconds() || burned > 1.5*elapsed.Seconds() {
		t.Fatalf("cpu delta = %.3fs for %.3fs busy wall — units are wrong", burned, elapsed.Seconds())
	}
	t.Logf("burned %.3fs cpu over %.3fs wall — units check out", burned, elapsed.Seconds())
}
