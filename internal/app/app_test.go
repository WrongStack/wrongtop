package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/dockerclient"
)

func TestNewTabsRespectModules(t *testing.T) {
	all := config.Default()
	m := New(all, "", "test")
	want := []string{
		"⌂ DASHBOARD", "☰ PROCESSES", "▣ DOCKER", "▤ DISKS", "⇅ NETWORK",
		"♨ SENSORS", "⇄ CONNECTIONS",
	}
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
	m = New(off, "", "test")
	want = []string{"⌂ DASHBOARD", "▤ DISKS", "⇅ NETWORK"}
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
	m := New(cfg, "", "test")
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
	m := New(cfg, "", "test")
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
	// net rates render in the compact short form (no "/s") so the chips
	// keep their width budget on narrow terminals
	for _, want := range []string{"cpu", "34%", "mem 62%", "↓1.2 Mb", "↑340.0 Kb"} {
		if !strings.Contains(out, want) {
			t.Errorf("status bar summary missing %q: %q", want, out)
		}
	}
}

// TestTabBarAlertChips pins the new alert placement: threshold crossings
// surface as chips on the tab bar's right zone (with the clock), on
// every tab, and clicking that zone opens the alert history.
func TestTabBarAlertChips(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, "", "test")
	m.width = 120
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time: time.Now(),
		CPU:  collector.CPU{Percent: 95}, // crit is 90
	}})

	out := m.tabBarView()
	if !strings.Contains(out, "⚠ CPU 95%") {
		t.Errorf("tab bar missing the alert chip: %q", out)
	}
	if m.alertZoneStart < 0 || m.alertZoneStart >= m.width {
		t.Errorf("alert zone not mapped for clicks: start=%d width=%d", m.alertZoneStart, m.width)
	}
	if _, ok := m.tabAt(m.width - 1); ok {
		t.Error("right edge of the tab bar must not resolve to a tab")
	}
	// chip text is softened toward the background; check the crit color leaks through
	if !strings.Contains(out, ";48;2;") {
		t.Error("alert chip lost its background color")
	}
}

// TestTabBarIconFirstDegradation pins the width-pressure ladder: before
// anything is dropped, inactive tabs collapse to their icons while the
// active tab keeps its label — and the recorded click bounds follow.
func TestTabBarIconFirstDegradation(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, "", "test")
	m.width = 44 // too narrow for five full labels plus the clock

	out := m.tabBarView()
	if !strings.Contains(out, "DASHBOARD") {
		t.Errorf("active tab should keep its label: %q", stripANSITest(out))
	}
	if strings.Contains(out, "PROCESSES") {
		t.Errorf("inactive tabs should collapse to their icons: %q", stripANSITest(out))
	}
	activeW := m.tabBounds[0][1] - m.tabBounds[0][0]
	idleW := m.tabBounds[1][1] - m.tabBounds[1][0]
	if idleW > 4 || idleW >= activeW {
		t.Errorf("compact tab bounds should cover the icon only: active=%d idle=%d", activeW, idleW)
	}
}

// stripANSITest removes SGR sequences so pinned text is readable.
func stripANSITest(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// TestStatusBarDockedToBottom pins the chrome contract: whatever the
// active tab renders (here: the dashboard's pre-sample waiting box, a
// couple of lines), the frame is always exactly the full screen and the
// status bar sits on the last row — never right after short content.
func TestStatusBarDockedToBottom(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, "", "test")
	m.width, m.height = 100, 30
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	out := m.frame()
	lines := strings.Split(out, "\n")
	if len(lines) != 30 {
		t.Fatalf("frame has %d lines, want the full 30", len(lines))
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, "WRONGTOP") {
		t.Errorf("status bar not on the bottom row: %q", last)
	}
	if !strings.Contains(out, "waiting for samples") {
		t.Errorf("short content missing from the frame:\n%s", stripANSITest(out))
	}
}

// TestResizeAcrossColumnThresholds is the resize-crash regression: a
// bubbles table panics in renderRow when its column count changes while
// stale rows are in place, and WindowSizeMsg reaches every tab. Stomp
// the terminal width back and forth across all five tabs' thresholds
// (docker 146/118/94, processes 112, network 100/112/88, disks 90/76,
// conns 108/84) — it must never panic.
func TestResizeAcrossColumnThresholds(t *testing.T) {
	m := New(config.Default(), "", "test")
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	m.Update(dockerclient.UpdateMsg{Client: &dockerclient.Client{}, Containers: []dockerclient.Container{{
		ID: "abc", Name: "web", Image: "nginx", State: "running",
		CPU: 12, Mem: 1 << 28, MemPct: 4, Status: "Up 2 hours (healthy)",
	}}})
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time:    time.Now(),
		CPU:     collector.CPU{Percent: 40, Cores: []float64{10, 20, 30, 40, 50, 60}},
		Procs:   []collector.Proc{{PID: 1, Name: "x", CPU: 5, Mem: 1, RSS: 1 << 20, User: "u", State: "S"}},
		DiskIOs: []collector.DiskIO{{Name: "disk0", ReadBytes: 1, WriteBytes: 1, BusyPercent: 9}},
		Nets:    []collector.NetIface{{Name: "en0", RxRate: 10, TxRate: 10}},
		Conns:   []collector.Conn{{Local: "a:1", Remote: "b:2", State: "ESTABLISHED", PID: 1}},
		Disks:   []collector.Disk{{Device: "/dev/s1", Mountpoint: "/", Total: 1 << 30, Percent: 9}},
	}})
	for _, w := range []int{200, 146, 145, 118, 117, 112, 111, 109, 100, 99, 90, 89, 88, 76, 60, 200, 76} {
		m.Update(tea.WindowSizeMsg{Width: w, Height: 40})
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
	mOn := New(on, "", "test")
	if got := flattenCmd(t, mOn.Init()); len(got) != 3 {
		t.Errorf("Init with docker module: got %d cmds, want 3", len(got))
	}

	off := config.Default()
	off.Modules = config.Modules{}
	mOff := New(off, "", "test")
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

// TestOverlaysSwallowContentClicks pins the modal contract: while the
// help or alerts overlay is open, content-area clicks must not reach
// the hidden tab underneath — the KeyPressMsg case already swallows
// keys there. Regression: the MouseClickMsg case guarded forwarding
// with !helpMode && !alertsMode but fell through to the tab dispatch
// when the guard failed, so a click mutated the hidden table's
// selection invisibly.
func TestOverlaysSwallowContentClicks(t *testing.T) {
	snap := collector.Snapshot{Time: time.Now()}
	for i := 1; i <= 6; i++ {
		snap.Procs = append(snap.Procs, collector.Proc{
			PID: int32(100 + i), Name: fmt.Sprintf("proc%02d", i), User: "u", State: "S",
		})
	}
	m := New(config.Default(), "", "test")
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	m.Update(collector.SnapshotMsg{Snap: snap})
	m.Update(key("2")) // PROCESSES tab

	content := func() string {
		lines := strings.Split(m.frame(), "\n")
		if len(lines) < 3 {
			t.Fatalf("frame has %d lines, want tab bar + content + status", len(lines))
		}
		// drop the tab-bar row (clock, alert pulse) and the status bar:
		// only stable tab content takes part in the comparison
		return strings.Join(lines[1:len(lines)-1], "\n")
	}

	r0 := content()
	m.Update(tea.MouseClickMsg{X: 10, Y: 5, Button: tea.MouseLeft})
	r1 := content()
	if r1 == r0 {
		t.Fatal("control click did not move the processes selection; click path ineffective")
	}

	for _, o := range []struct{ name, toggle string }{
		{"alerts", "a"},
		{"help", "?"},
	} {
		m.Update(key(o.toggle))
		m.Update(tea.MouseClickMsg{X: 10, Y: 7, Button: tea.MouseLeft})
		m.Update(key(o.toggle))
		if got := content(); got != r1 {
			t.Errorf("%s overlay: content click leaked through and moved the hidden selection", o.name)
		}
	}

	// the tab-bar row stays clickable under an overlay (keys 1-9 also
	// keep working there), so switching tabs must not regress
	m.Update(key("a")) // alerts overlay open
	m.Update(tea.MouseClickMsg{X: m.tabBounds[0][0], Y: 0, Button: tea.MouseLeft})
	if m.active != 0 {
		t.Errorf("tab-bar click under overlay did not switch tabs (active = %d)", m.active)
	}
}
