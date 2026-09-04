package processes

import (
	"testing"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/ersinkoc/wrongtop/internal/collector"
	"github.com/ersinkoc/wrongtop/internal/config"
	"github.com/ersinkoc/wrongtop/internal/theme"
)

// keyPress builds a printable KeyPressMsg for tests.
func keyPress(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func escKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEscape} }

func testSnapshot() collector.Snapshot {
	return collector.Snapshot{
		Time: time.Now(),
		Procs: []collector.Proc{{
			PID: 42, Name: "testproc", CPU: 5, Mem: 1.5, RSS: 1024,
			User: "tester", State: "S", Threads: 2,
		}},
	}
}

func TestConfiguredKeys(t *testing.T) {
	cfg := config.Default()
	cfg.Keys = config.Keys{Kill: "d", Filter: "x"}
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(120, 30)
	m.Update(collector.SnapshotMsg{Snap: testSnapshot()})

	m.Update(keyPress("x"))
	if !m.editing {
		t.Fatalf("configured filter key %q did not open the filter", cfg.Keys.Filter)
	}
	m.Update(escKey())
	if m.editing || m.filter != "" {
		t.Fatal("esc did not close the filter")
	}

	m.Update(keyPress("d"))
	if m.confirm == nil || m.confirm.pid != 42 || m.confirm.sigs[m.confirm.sig].Name != "TERM" {
		t.Fatalf("configured kill key %q did not open terminate prompt: %+v", cfg.Keys.Kill, m.confirm)
	}

	m.Update(keyPress("D"))
	if m.confirm == nil || m.confirm.sigs[m.confirm.sig].Name != "KILL" {
		t.Fatalf("uppercase kill key did not preselect SIGKILL: %+v", m.confirm)
	}

	m.Update(escKey())
	if m.confirm != nil {
		t.Fatal("esc did not cancel the kill prompt")
	}
}

func TestDefaultKeys(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(120, 30)
	m.Update(collector.SnapshotMsg{Snap: testSnapshot()})

	m.Update(keyPress("/"))
	if !m.editing {
		t.Fatal("default filter key / did not open the filter")
	}
	m.Update(escKey())

	m.Update(keyPress("k"))
	if m.confirm == nil || m.confirm.sigs[m.confirm.sig].Name != "TERM" {
		t.Fatal("default kill key k did not open terminate prompt")
	}

	// the picker cycles through the platform's signal list
	before := m.confirm.sig
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.confirm.sig != (before+1)%len(m.confirm.sigs) {
		t.Fatalf("right arrow did not advance the signal: %d", m.confirm.sig)
	}
	m.Update(escKey())
	if m.confirm != nil {
		t.Fatal("esc did not cancel the kill prompt")
	}
}

func TestTreeView(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(120, 30)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time: time.Now(),
		Procs: []collector.Proc{
			{PID: 1, PPID: 0, Name: "launchd"},
			{PID: 10, PPID: 1, Name: "parent"},
			{PID: 20, PPID: 10, Name: "child"},
		},
	}})

	m.Update(keyPress("t"))
	if !m.tree {
		t.Fatal("t did not enable tree mode")
	}
	if len(m.rows) != 3 {
		t.Fatalf("tree rows = %d, want 3", len(m.rows))
	}
	if m.rows[0].Proc.Name != "launchd" || m.rows[0].Depth != 0 {
		t.Fatalf("unexpected first row: %+v", m.rows[0])
	}
	if m.rows[1].Depth != 1 || m.rows[2].Depth != 2 {
		t.Fatalf("depths not threaded: %d %d", m.rows[1].Depth, m.rows[2].Depth)
	}

	// collapse the selected parent: cursor sits on launchd, move down one
	m.Update(keyPress("j")) // bubbles table maps j? no — use down arrow
	if m.table.Cursor() != 1 {
		t.Fatalf("cursor at %d, want 1", m.table.Cursor())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if !m.collapsed[10] {
		t.Fatal("left arrow did not collapse the parent")
	}
	if len(m.rows) != 2 {
		t.Fatalf("collapsed tree shows %d rows, want 2", len(m.rows))
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.collapsed[10] {
		t.Fatal("right arrow did not expand the parent")
	}
	if len(m.rows) != 3 {
		t.Fatalf("expanded tree shows %d rows, want 3", len(m.rows))
	}
}

func TestFooterShowsConfiguredKeys(t *testing.T) {
	cfg := config.Default()
	cfg.Keys = config.Keys{Kill: "d", Filter: "x"}
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(120, 30)
	m.Update(collector.SnapshotMsg{Snap: testSnapshot()})

	out := m.View()
	for _, want := range []string{"x", "d", "D"} {
		if !contains(out, want) {
			t.Errorf("footer missing configured key %q", want)
		}
	}
	if contains(out, "k") && !contains(out, "kill") {
		// the old default kill key must not leak into the footer hints
		t.Errorf("footer still references default kill key")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
