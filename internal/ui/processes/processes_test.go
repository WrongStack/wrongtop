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
	if m.confirm == nil || m.confirm.pid != 42 || m.confirm.force {
		t.Fatalf("configured kill key %q did not open terminate prompt: %+v", cfg.Keys.Kill, m.confirm)
	}

	m.Update(keyPress("D"))
	if m.confirm == nil || !m.confirm.force {
		t.Fatalf("uppercase kill key did not upgrade prompt to force: %+v", m.confirm)
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
	if m.confirm == nil || m.confirm.force {
		t.Fatal("default kill key k did not open terminate prompt")
	}
	m.Update(escKey())
	if m.confirm != nil {
		t.Fatal("esc did not cancel the kill prompt")
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
