package docker

import (
	"fmt"
	"testing"
	"time"

	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/dockerclient"
	"github.com/wrongstack/wrongtop/internal/theme"
)

// TestUpdateRatesPrunesPrevToCurrentContainers pins the rate-diff state
// contract: prev holds only the current poll's containers. It is the
// diff basis for the NEXT poll; entries for vanished containers can
// never produce a rate again, so keeping them is a slow unbounded leak
// on churny hosts (restarts, ephemeral build containers).
func TestUpdateRatesPrunesPrevToCurrentContainers(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))

	// churn: every poll the old container is gone and a fresh ID appears
	for i := 0; i < 300; i++ {
		m.Update(dockerclient.UpdateMsg{
			Containers: []dockerclient.Container{{
				ID: fmt.Sprintf("ctr-%03d", i), Name: "c", State: "running",
			}},
		})
	}
	if len(m.prev) != 1 {
		t.Fatalf("FAIL: after 300 single-container polls prev holds %d entries, want 1 (only the current container)", len(m.prev))
	}

	// rates must still diff across polls for a surviving container
	m.Update(dockerclient.UpdateMsg{
		Containers: []dockerclient.Container{{
			ID: "stable", Name: "c", State: "running", NetRx: 100,
		}},
	})
	time.Sleep(5 * time.Millisecond) // guarantee dt > 0 for the diff
	m.Update(dockerclient.UpdateMsg{
		Containers: []dockerclient.Container{{
			ID: "stable", Name: "c", State: "running", NetRx: 300,
		}},
	})
	if r := m.rates["stable"]; r[0] <= 0 {
		t.Fatalf("FAIL: rx rate for the surviving container = %v, want > 0 (diff across two polls)", r[0])
	}
	if len(m.prev) != 1 {
		t.Fatalf("FAIL: prev holds %d entries after the rate check, want 1", len(m.prev))
	}
}
