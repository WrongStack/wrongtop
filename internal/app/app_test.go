package app

import (
	"strings"
	"testing"

	"charm.land/bubbletea/v2"

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
