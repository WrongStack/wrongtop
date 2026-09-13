package collector

// collectHost wraps the host identity fetch in idMu with a retry on
// empty: the identity call is context-bound, and both the TUI tick and
// the serve ticker give Collect a 2-second budget that collectHost —
// running late in the snapshot — can exhaust. A failed fetch must be
// retried on the next Collect; a one-shot init would zero the hostname,
// OS, platform and kernel for the whole session after a single busy
// first snapshot.

import (
	"context"
	"testing"
	"time"
)

func TestHostIdentitySurvivesFailedFirstCollect(t *testing.T) {
	c := New(time.Second)

	// first Collect with a budget already spent: the identity fetch runs
	// against a dead context, exactly as a 2s Collect timeout that
	// expires mid-snapshot hands collectHost a spent context
	dead, cancel := context.WithCancel(context.Background())
	cancel()
	c.Collect(dead)

	// healthy budget: every other section recovers — identity must too
	snap := c.Collect(context.Background())
	if snap.Host.Hostname == "" {
		t.Fatalf("host identity lost permanently after one failed first Collect — Hostname=%q OS=%q Kernel=%q Platform=%q (init not retried)",
			snap.Host.Hostname, snap.Host.OS, snap.Host.Kernel, snap.Host.Platform)
	}
}
