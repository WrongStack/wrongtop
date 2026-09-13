package collector

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestCachedUsersRaceFree pins the concurrency contract of Collector
// ("safe for concurrent use"): cachedUsers is written by collectSlow
// under slowMu, so collectHost must read it under the same lock. The
// TUI can overlap two Collect calls — its tick handler re-arms
// collectCmd on the wall clock (app.go tickMsg), so a Collect that
// outlasts its refresh interval races the next one. Run under -race
// (the project gate): reverting the lock fires the detector within the
// first forced refresh.
func TestCachedUsersRaceFree(t *testing.T) {
	c := New(250 * time.Millisecond)
	base := time.Now()
	ctx := context.Background()

	// Reader: loops collectHost — the read side of the contract.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = c.collectHost(ctx)
			}
		}
	}()

	// Writer: each call advances the fake clock past the 5s slow
	// interval, forcing the refresh branch and its cachedUsers write.
	for i := 1; i <= 10; i++ {
		c.collectSlow(ctx, base.Add(time.Duration(i)*10*time.Second))
	}
	close(stop)
	wg.Wait()

	// Production shape: concurrent exported Collect calls (the TUI tick
	// overlap) must also stay race-free.
	var hammer sync.WaitGroup
	for i := 0; i < 4; i++ {
		hammer.Add(1)
		go func() {
			defer hammer.Done()
			deadline := time.Now().Add(300 * time.Millisecond)
			for time.Now().Before(deadline) {
				_ = c.Collect(ctx)
			}
		}()
	}
	hammer.Wait()

	// Consistency under the contract: the host section reports the
	// cached user count.
	c.slowMu.Lock()
	want := c.cachedUsers
	c.slowMu.Unlock()
	if got := c.collectHost(ctx).Users; got != want {
		t.Errorf("collectHost().Users = %d, cachedUsers = %d", got, want)
	}
}
