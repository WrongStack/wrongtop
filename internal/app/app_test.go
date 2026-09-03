package app

import (
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/ersinkoc/wrongtop/internal/collector"
	"github.com/ersinkoc/wrongtop/internal/config"
	"github.com/ersinkoc/wrongtop/internal/dockerclient"
)

func TestNewTabsRespectModules(t *testing.T) {
	all := config.Default()
	m := New(all, "test")
	want := []string{"DASHBOARD", "PROCESSES", "DOCKER", "DISKS", "NETWORK"}
	if len(m.tabs) != len(want) {
		t.Fatalf("default modules: got %d tabs, want %d", len(m.tabs), len(want))
	}
	for i, w := range want {
		if got := m.tabs[i].Title(); got != w {
			t.Errorf("tab %d: got %q, want %q", i, got, w)
		}
	}

	off := config.Default()
	off.Modules = config.Modules{}
	m = New(off, "test")
	want = []string{"DASHBOARD", "DISKS", "NETWORK"}
	if len(m.tabs) != len(want) {
		t.Fatalf("optional modules off: got %d tabs, want %d", len(m.tabs), len(want))
	}
	for i, w := range want {
		if got := m.tabs[i].Title(); got != w {
			t.Errorf("tab %d: got %q, want %q", i, got, w)
		}
	}
}

func TestStatusBarShowsDynamicTabRange(t *testing.T) {
	cfg := config.Default()
	cfg.Modules = config.Modules{}
	m := New(cfg, "test")
	m.width = 120
	out := m.statusBarView()
	if !strings.Contains(out, "1-3") {
		t.Errorf("status bar missing dynamic tab range 1-3: %q", out)
	}
}

// TestStatusBarLiveSummary pins the btop-style bottom line: once a
// snapshot has arrived the bar shows cpu/mem percentages and net rates.
func TestStatusBarLiveSummary(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, "test")
	m.width = 120

	if out := m.statusBarView(); strings.Contains(out, "cpu") {
		t.Error("status bar should not show a summary before any sample")
	}

	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time: time.Now(),
		CPU:  collector.CPU{Percent: 34},
		Mem:  collector.Mem{Percent: 62},
		Nets: []collector.NetIface{{Name: "en0", RxRate: 1.2e6, TxRate: 340e3}},
	}})
	out := m.statusBarView()
	for _, want := range []string{"cpu 34%", "mem 62%", "1.2 Mb/s", "340.0 Kb/s"} {
		if !strings.Contains(out, want) {
			t.Errorf("status bar summary missing %q: %q", want, out)
		}
	}
}

// flattenCmd unwraps a tea.Batch into its sub-commands without running
// them, so tests can count scheduled work cheaply.
func flattenCmd(t *testing.T, cmd tea.Cmd) []tea.Cmd {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		return batch
	}
	return []tea.Cmd{cmd}
}

func TestDockerLifecycleGatedByModule(t *testing.T) {
	on := config.Default() // Modules.Docker: true
	mOn := New(on, "test")
	if got := flattenCmd(t, mOn.Init()); len(got) != 3 {
		t.Errorf("Init with docker module: got %d cmds, want 3", len(got))
	}

	off := config.Default()
	off.Modules = config.Modules{}
	mOff := New(off, "test")
	if got := flattenCmd(t, mOff.Init()); len(got) != 2 {
		t.Errorf("Init without docker module: got %d cmds, want 2", len(got))
	}

	// a connected client must not be polled when the module is disabled
	mOff.docker = &dockerclient.Client{}
	if _, cmd := mOff.Update(tickMsg{}); len(flattenCmd(t, cmd)) != 2 {
		t.Error("tick without docker module must not schedule dockerListCmd")
	}

	mOn.docker = &dockerclient.Client{}
	if _, cmd := mOn.Update(tickMsg{}); len(flattenCmd(t, cmd)) != 3 {
		t.Error("tick with docker module and client must schedule dockerListCmd")
	}
}
