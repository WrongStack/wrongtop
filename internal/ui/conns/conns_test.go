package conns

import (
	"strings"
	"testing"
	"time"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/theme"
)

func newModel(t *testing.T, w, h int) *Model {
	t.Helper()
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(w, h)
	return m
}

// TestViewRendersHead pins the summary line: totals, established and
// listener counts with state coloring.
func TestViewRendersHead(t *testing.T) {
	m := newModel(t, 120, 30)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time: time.Now(),
		Conns: []collector.Conn{
			{Local: "10.0.0.1:5000", Remote: "1.1.1.1:443", State: "ESTABLISHED", PID: 42},
			{Local: "*:8080", State: "LISTEN", PID: 7},
			{Local: "10.0.0.1:5001", Remote: "1.1.1.1:80", State: "TIME_WAIT"},
		},
		Procs: []collector.Proc{{PID: 42, Name: "curl"}, {PID: 7, Name: "myserver"}},
	}})
	out := m.View()
	for _, want := range []string{"3 conns", "1 established", "1 listen", "ESTABLISHED", "LISTEN", "TIME_WAIT", "curl", "myserver"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
}

// TestEmptyState pins the centered placeholder when the table is empty.
func TestEmptyState(t *testing.T) {
	m := newModel(t, 120, 30)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{Time: time.Now()}})
	if out := m.View(); !strings.Contains(out, "no TCP connections") {
		t.Errorf("empty state missing:\n%s", out)
	}
}

// TestProcessColumnsFollowPIDAvailability: when no connection carries a
// PID (linux kernel tables) the PROCESS column must disappear — rows and
// headers rebuilt together.
func TestProcessColumnsFollowPIDAvailability(t *testing.T) {
	m := newModel(t, 120, 30)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time:  time.Now(),
		Conns: []collector.Conn{{Local: "a:b", State: "ESTABLISHED", PID: 42}},
		Procs: []collector.Proc{{PID: 42, Name: "x"}},
	}})
	if !strings.Contains(m.View(), "PROCESS") {
		t.Error("PROCESS column missing while PIDs resolve")
	}
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time:  time.Now(),
		Conns: []collector.Conn{{Local: "a:b", State: "ESTABLISHED"}},
	}})
	if strings.Contains(m.View(), "PROCESS") {
		t.Error("PROCESS column kept without PIDs")
	}
}
