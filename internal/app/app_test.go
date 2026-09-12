package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/dockerclient"
	"github.com/wrongstack/wrongtop/internal/remote"
	"github.com/wrongstack/wrongtop/internal/ui"
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

// TestAlertsOverlayBoundedToTerminal pins the alerts overlay
// containment: event text carries snapshot values (the remote wire can
// deliver extremes) and the overlay box grows to fit its content, so
// every history line must be truncated to the terminal width — a
// hostile snapshot must not wrap the fixed-height frame.
func TestAlertsOverlayBoundedToTerminal(t *testing.T) {
	m := New(config.Default(), "", "test")
	m.width = 120
	snap := collector.Snapshot{Time: time.Now()}
	snap.CPU.Percent = 1e300
	snap.Mem.Percent = 1e300
	m.trackAlerts(snap)
	if len(m.alerts) == 0 {
		t.Fatal("extreme snapshot produced no alerts")
	}
	for i, line := range strings.Split(m.alertsOverlayView(), "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Fatalf("alerts overlay line %d is %d cells wide, want <= %d", i, w, m.width)
		}
	}
}

// TestRemoteLostBannerReplacesStatus pins the end-of-stream contract for
// remote mode: a dead stream must stay visible instead of fading with a
// 3-second flash. The status bar becomes a persistent connection-lost
// banner carrying the frozen-at time and the stream error, bounded to
// the terminal width; local mode is unaffected.
func TestRemoteLostBannerReplacesStatus(t *testing.T) {
	m := NewRemote(config.Default(), "", "test", nil, "host:1234")
	m.width = 120
	m.latest = collector.Snapshot{Time: time.Date(2026, 9, 10, 15, 4, 5, 0, time.Local)}
	if _, err := m.Update(remoteEndedMsg{err: errors.New("connection reset by peer")}); err != nil {
		t.Fatalf("Update(remoteEndedMsg): %v", err)
	}
	if m.flash != "" {
		t.Errorf("end-of-stream must not rely on the transient flash, got %q", m.flash)
	}
	bar := m.statusBarView()
	plain := ui.StripANSI(bar)
	if !strings.Contains(plain, "REMOTE DISCONNECTED") {
		t.Errorf("status bar missing the connection-lost banner: %q", plain)
	}
	if !strings.Contains(plain, "15:04:05") {
		t.Errorf("banner missing the frozen-at time: %q", plain)
	}
	if !strings.Contains(plain, "connection reset by peer") {
		t.Errorf("banner missing the stream error: %q", plain)
	}
	for i, line := range strings.Split(bar, "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Fatalf("banner line %d is %d cells wide, want <= %d", i, w, m.width)
		}
	}

	local := New(config.Default(), "", "test")
	local.width = 120
	if out := ui.StripANSI(local.statusBarView()); strings.Contains(out, "REMOTE DISCONNECTED") {
		t.Errorf("local status bar must not show the banner: %q", out)
	}
}

// TestReloadConfigPreservesModuleSet pins the reload contract: reload
// applies only what can change live (theme, refresh, thresholds, keys).
// The module set is structural — tabs were built once at startup — and
// has live readers (the docker polling gate, the help overlay), so a
// rewritten config file must not swap it out from under them.
func TestReloadConfigPreservesModuleSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	start := "theme: tokyo-night\n" // modules absent → config defaults, all on
	if err := os.WriteFile(path, []byte(start), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Modules.Docker || !cfg.Modules.Processes {
		t.Fatalf("precondition: default modules should be on, got %+v", cfg.Modules)
	}
	m := New(cfg, path, "test")

	changed := "theme: nord\nmodules:\n  docker: false\n  processes: false\n"
	if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	m.reloadConfig()

	if !m.cfg.Modules.Docker || !m.cfg.Modules.Processes {
		t.Fatalf("reload mutated the module set: %+v", m.cfg.Modules)
	}
	if m.cfg.Theme != "nord" || m.theme.Palette.Name != "nord" {
		t.Errorf("reload must still apply the theme, got cfg %q / palette %q",
			m.cfg.Theme, m.theme.Palette.Name)
	}
}

// The remote view must stay read-only across a live config reload:
// remote snapshot pids are not local pids, so a rewritten file without
// read_only must not re-arm signaling against the local machine
// (NewRemote forces the flag at construction). Local mode keeps the
// file's read_only value live-reloadable, both ways.
func TestReloadConfigKeepsRemoteReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	start := "theme: tokyo-night\n" // read_only absent → file value false
	if err := os.WriteFile(path, []byte(start), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	m := NewRemote(cfg, path, "test", &remote.Client{}, "host:1")
	if !cfg.ReadOnly {
		t.Fatal("precondition: NewRemote must force ReadOnly")
	}

	if err := os.WriteFile(path, []byte("theme: nord\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.reloadConfig()

	if !cfg.ReadOnly {
		t.Fatal("remote reload reset ReadOnly: signaling re-armed on remote pids")
	}
	if m.cfg.Theme != "nord" || m.theme.Palette.Name != "nord" {
		t.Errorf("remote reload must still apply the file, got cfg %q / palette %q",
			m.cfg.Theme, m.theme.Palette.Name)
	}
	if m.cfg.Modules.Docker {
		t.Error("remote reload must keep docker disabled")
	}

	// contrast: local mode keeps the documented live-reload semantics
	localPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(localPath, []byte("read_only: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	localCfg, err := config.Load(localPath)
	if err != nil {
		t.Fatal(err)
	}
	local := New(localCfg, localPath, "test")
	if !localCfg.ReadOnly {
		t.Fatal("precondition: local read_only: true must load")
	}
	if err := os.WriteFile(localPath, []byte("read_only: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	local.reloadConfig()
	if localCfg.ReadOnly {
		t.Error("local reload must still apply the file's read_only value")
	}
}

// Every help row is parsed by helpView with strings.Cut(l, "  "): the
// first double space separates key from description, and a row without
// one renders as a fused key blob — no description column, misaligned
// with the rest of its section.
func TestHelpRowsUseKeyDescColumns(t *testing.T) {
	m := New(config.Default(), "", "test")
	for _, sec := range m.helpSections() {
		for _, l := range sec.lines {
			if _, _, found := strings.Cut(l, "  "); !found {
				t.Errorf("help row %q in section %s has no key/desc separator", l, sec.title)
			}
		}
	}
	view := stripANSITest(m.helpView())
	for _, want := range []string{
		"q         quit (or ctrl+c)",
		"←→        switch usage / I/O table",
		"←→        collapse / expand tree",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("help view missing aligned row %q", want)
		}
	}
}

// The status bar must stay inside the terminal: the chip-drop loop
// spends the width budget lowest-priority-first, and the centering gap
// must absorb whatever is left — a forced filler cell made the bar
// render width+1 columns at every exact-fit width.
func TestStatusBarRespectsTerminalWidth(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, "", "test")
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{
		Time: time.Now(),
		CPU:  collector.CPU{Percent: 42},
		Mem:  collector.Mem{Percent: 61},
		Nets: []collector.NetIface{{Name: "en0", RxRate: 1e6, TxRate: 2e5, RxTotal: 1, TxTotal: 1}},
	}})
	for w := 30; w <= 160; w++ {
		m.width = w
		if n := lipgloss.Width(m.statusBarView()); n > w {
			t.Errorf("width %d: status bar renders %d columns", w, n)
		}
	}
}

// The flash is transient feedback rendered into the one-line status
// bar: config errors carry file paths and yaml newlines, so it must be
// flattened and truncated to the bar's remaining budget — and dropped
// entirely when no room is left.
func TestStatusBarFlashStaysInBudget(t *testing.T) {
	m := New(config.Default(), "", "test")
	m.flash = "config error: wrongtop: parsing /var/folders/xx/T/config.yaml: yaml: unmarshal errors:\n  line 1: cannot unmarshal !!str `not-a-map` into config.Modules"
	m.flashUntil = time.Now().Add(time.Minute)
	for w := 40; w <= 120; w += 10 {
		m.width = w
		bar := m.statusBarView()
		if strings.Contains(bar, "\n") {
			t.Fatalf("width %d: flash newline split the status bar: %q", w, bar)
		}
		if n := lipgloss.Width(bar); n > w {
			t.Errorf("width %d: status bar renders %d columns", w, n)
		}
	}
	m.width = 120
	if !strings.Contains(stripANSITest(m.statusBarView()), "config error") {
		t.Errorf("flash should stay visible (truncated), got %q", stripANSITest(m.statusBarView()))
	}
}

// The help box is content-sized and can outgrow the terminal; every
// overlay line must be truncated to the width so the frame never
// renders wider than the terminal.
func TestHelpOverlayFitsTerminalWidth(t *testing.T) {
	m := New(config.Default(), "", "test")
	m.helpMode = true
	for w := 40; w <= 120; w += 10 {
		m.Update(tea.WindowSizeMsg{Width: w, Height: 30})
		for _, l := range strings.Split(m.View().Content, "\n") {
			if n := lipgloss.Width(l); n > w {
				t.Errorf("width %d: help frame line renders %d columns: %q", w, n, stripANSITest(l))
				break
			}
		}
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
