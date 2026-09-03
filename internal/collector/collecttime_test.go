package collector

import (
	"context"
	"testing"
	"time"
)

func TestCollectTiming(t *testing.T) {
	c := &Collector{}
	start := time.Now()
	snap := c.Collect(context.Background())
	t.Logf("first collect: %v (%d procs)", time.Since(start), len(snap.Procs))
	start = time.Now()
	snap = c.Collect(context.Background())
	t.Logf("second collect: %v (%d procs)", time.Since(start), len(snap.Procs))
	start = time.Now()
	snap = c.Collect(context.Background())
	t.Logf("third collect: %v (%d procs)", time.Since(start), len(snap.Procs))
	if len(snap.Procs) == 0 {
		t.Fatal("no processes collected")
	}
	withName := 0
	for _, p := range snap.Procs {
		if p.Name != "" {
			withName++
		}
	}
	t.Logf("procs with name: %d", withName)
}
