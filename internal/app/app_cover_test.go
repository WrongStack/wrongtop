package app

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/dockerclient"
	"github.com/wrongstack/wrongtop/internal/remote"
	"github.com/wrongstack/wrongtop/internal/theme"
	"github.com/wrongstack/wrongtop/internal/ui"
)

func key(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: []rune(s)[0], Text: s} }

// cmdTab answers every Update with a scheduled command, so the app's
// tab loops (snapshot and docker fan-out) take their command-collecting
// branches.
type cmdTab struct{ ui.Tab }

func (cmdTab) Update(tea.Msg) tea.Cmd { return tea.Quit }

func (t cmdTab) SetVisible(bool)       {}
func (t cmdTab) SetSize(int, int)      {}
func (t cmdTab) SetTheme(*theme.Theme) {}
func (t cmdTab) View() string          { return "" }

// sized builds a model with a laid-out frame so tab and alert-zone
// bounds are populated for mouse tests.
func sized(t *testing.T, cfg *config.Config, w, h int) *Model {
	t.Helper()
	m := New(cfg, "", "test")
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m.frame()
	return m
}

func fullSnapshot() collector.Snapshot {
	return collector.Snapshot{
		Time:    time.Now(),
		CPU:     collector.CPU{Percent: 34, Cores: []float64{10, 20}},
		Mem:     collector.Mem{Percent: 62, Total: 16 << 30, Used: 10 << 30},
		Nets:    []collector.NetIface{{Name: "en0", RxRate: 1.2e6, TxRate: 340e3}},
		Sensors: []collector.Sensor{{Name: "TC0P", TempC: 65}},
		Battery: &collector.Battery{Percent: 80, Charging: true},
	}
}

func TestSetActiveBounds(t *testing.T) {
	m := New(config.Default(), "", "test")
	for _, i := range []int{-1, len(m.tabs), 0} { // out of range and no-op
		m.setActive(i)
		if m.active != 0 {
			t.Fatalf("setActive(%d) moved to %d", i, m.active)
		}
	}
	m.setActive(2)
	if m.active != 2 {
		t.Fatalf("setActive(2): active=%d", m.active)
	}
}

func TestRemoteModelDisablesLocalPolling(t *testing.T) {
	cfg := config.Default()
	m := NewRemote(cfg, "", "v", &remote.Client{}, "host:1")
	if cfg.Modules.Docker {
		t.Error("remote mode must disable docker")
	}
	if !cfg.ReadOnly {
		t.Error("remote mode must be read-only: remote pids must never be signaled locally")
	}
	if m.Init() != nil {
		t.Error("remote Init must not schedule local collection")
	}
	if _, cmd := m.Update(tickMsg{}); cmd != nil {
		t.Error("remote tick must not poll")
	}
	m.width = 120
	if out := m.statusBarView(); !strings.Contains(out, "REMOTE host:1") {
		t.Errorf("status bar missing remote chip: %q", stripANSITest(out))
	}
}

// fakeDocker serves the two Engine API calls the app issues on connect
// and poll: ping and an empty container list.
func fakeDocker(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			w.Header().Set("API-Version", "1.45")
			w.Header().Set("OSType", "linux")
			_, _ = w.Write([]byte("OK"))
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("DOCKER_HOST", "tcp://"+strings.TrimPrefix(srv.URL, "http://"))
}

func TestCommandsExecute(t *testing.T) {
	cfg := config.Default()
	cfg.Refresh = config.Duration(250 * time.Millisecond)
	m := New(cfg, "", "test")

	if _, ok := m.tickCmd()().(tickMsg); !ok {
		t.Error("tickCmd must yield a tickMsg")
	}
	snap, ok := m.collectCmd()().(collector.SnapshotMsg)
	if !ok || snap.Snap.Time.IsZero() {
		t.Error("collectCmd must yield a live snapshot")
	}

	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:1") // nothing listens: connect fails fast
	if msg := m.connectDockerCmd()().(dockerclient.UpdateMsg); msg.Err == nil || msg.Client != nil {
		t.Errorf("unreachable daemon must report an error: %+v", msg)
	}

	fakeDocker(t)
	msg := m.connectDockerCmd()().(dockerclient.UpdateMsg)
	if msg.Err != nil || msg.Client == nil {
		t.Fatalf("fake daemon should connect: %+v", msg)
	}
	defer func() { _ = msg.Client.Close() }()
	m.docker = msg.Client
	list := m.dockerListCmd()().(dockerclient.UpdateMsg)
	if list.Err != nil || list.Client != msg.Client || len(list.Containers) != 0 {
		t.Errorf("list against the fake daemon: %+v", list)
	}
}

func TestUpdateRouting(t *testing.T) {
	m := sized(t, config.Default(), 120, 30)

	m.frameCache = "keep"
	m.Update(tea.MouseMotionMsg{X: 1, Y: 1})
	if m.frameCache != "keep" {
		t.Error("mouse motion must not invalidate the frame")
	}

	m.Update(remoteEndedMsg{errors.New("boom")})
	if !m.remoteLost || m.flash != "" {
		t.Errorf("stream end must arm the persistent banner, not the flash (lost=%v flash=%q)", m.remoteLost, m.flash)
	}
	m.Update(remoteEndedMsg{})
	if !m.remoteLost {
		t.Error("stream end must arm the connection-lost banner")
	}

	if _, cmd := m.Update(dockerRetryMsg{}); cmd == nil {
		t.Error("retry must schedule a reconnect")
	}
	if _, cmd := m.Update(dockerclient.UpdateMsg{Err: errors.New("down")}); cmd == nil || m.docker != nil {
		t.Error("daemon-down must schedule a retry and keep no client")
	}
	dockerRetryDelay = time.Millisecond // the tick closure must fire
	_, retry := m.Update(dockerclient.UpdateMsg{Err: errors.New("down")})
	if _, ok := retry().(dockerRetryMsg); !ok {
		t.Error("retry tick must yield dockerRetryMsg")
	}
	dockerRetryDelay = 5 * time.Second
	client := &dockerclient.Client{}
	m.Update(dockerclient.UpdateMsg{Client: client})
	if m.docker != client {
		t.Error("client must be adopted")
	}

	// tab bar clicks: a tab, the alert/clock zone, an unhandled button
	m.Update(tea.MouseClickMsg{X: m.tabBounds[1][0], Y: 0, Button: tea.MouseLeft})
	if m.active != 1 {
		t.Errorf("click on tab 1 → active=%d", m.active)
	}
	m.Update(tea.MouseClickMsg{X: m.width - 1, Y: 0, Button: tea.MouseLeft})
	if !m.alertsMode {
		t.Error("click on the clock zone must open the alert history")
	}
	m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseRight})

	// content clicks reach the tab only when no overlay is up
	m.Update(tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft}) // alertsMode on
	m.alertsMode = false
	m.Update(tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft})

	// keys: global, swallowed by overlay, forwarded, and a foreign message
	if _, cmd := m.Update(key("q")); cmd == nil {
		t.Fatal("q must quit")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("q must yield QuitMsg")
	}
	m.helpMode = true
	if _, cmd := m.Update(key("x")); cmd != nil {
		t.Error("overlay must swallow non-global keys")
	}
	m.helpMode = false
	m.Update(key("x"))
	m.Update(struct{}{})

	// a tab that answers every message: the snapshot and docker fan-outs
	// collect the scheduled commands and batch them
	fan := New(config.Default(), "", "test")
	fan.tabs = []ui.Tab{cmdTab{fan.tabs[0]}}
	fan.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if _, cmd := fan.Update(collector.SnapshotMsg{Snap: collector.Snapshot{Time: time.Now()}}); cmd == nil {
		t.Error("snapshot fan-out must batch tab commands")
	}
	if _, cmd := fan.Update(dockerclient.UpdateMsg{Client: &dockerclient.Client{}}); cmd == nil {
		t.Error("docker fan-out must batch tab commands")
	}

	// the status-bar sparkline keeps a fixed-length cpu history
	for i := 0; i < cpuSparkSamples+4; i++ {
		fan.Update(collector.SnapshotMsg{Snap: collector.Snapshot{Time: time.Now(), CPU: collector.CPU{Percent: float64(i)}}})
	}
	if len(fan.cpuHist) != cpuSparkSamples {
		t.Errorf("cpu history not capped: %d", len(fan.cpuHist))
	}
}

func TestGlobalKeys(t *testing.T) {
	m := New(config.Default(), "", "test")
	n := len(m.tabs)

	m.Update(key("?"))
	if !m.helpMode {
		t.Error("? opens help")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.helpMode {
		t.Error("esc closes help")
	}
	m.Update(key("a"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.alertsMode {
		t.Error("esc closes alerts")
	}
	if _, handled := m.globalKey(tea.KeyPressMsg{Code: tea.KeyEscape}); handled {
		t.Error("esc with no overlay is not a global key")
	}
	if _, handled := m.globalKey(key("z")); handled {
		t.Error("z is not a global key")
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.active != 0 {
		t.Errorf("tab then shift+tab should return home, active=%d", m.active)
	}
	m.Update(key("3"))
	if m.active != 2 {
		t.Errorf("3 → active=%d", m.active)
	}
	m.Update(key("9")) // beyond the tab count: handled, no move
	if m.active != 2 {
		t.Errorf("9 with %d tabs moved to %d", n, m.active)
	}
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); cmd == nil {
		t.Error("ctrl+c must quit")
	}
}

func TestThemeCycleAndReload(t *testing.T) {
	names := theme.Names()
	cfg := config.Default()
	m := New(cfg, "", "test")
	cur := slices.Index(names, cfg.Theme)
	if cur < 0 {
		t.Fatalf("default theme %q not registered", cfg.Theme)
	}

	m.Update(key("T"))
	if want := names[(cur+1)%len(names)]; cfg.Theme != want || m.flash != "theme: "+want {
		t.Errorf("cycle: theme=%s flash=%q want %s", cfg.Theme, m.flash, want)
	}
	cfg.Theme = "no-such-theme"
	m.cycleTheme()
	if cfg.Theme != names[0] {
		t.Errorf("unknown theme should wrap to the first: %s", cfg.Theme)
	}

	// reload: error, then success through the default path env var
	m.cfgPath = t.TempDir()
	m.Update(key("R"))
	if !strings.HasPrefix(m.flash, "config error: ") {
		t.Errorf("reload from a directory: flash=%q", m.flash)
	}
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte("theme: "+names[2]+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.cfgPath = path
	m.reloadConfig()
	if m.flash != "config reloaded" || cfg.Theme != names[2] || m.theme.Palette.Name != names[2] {
		t.Errorf("reload: flash=%q theme=%s", m.flash, cfg.Theme)
	}
}

func TestTrackAlertsLifecycle(t *testing.T) {
	m := New(config.Default(), "", "test")
	now := time.Now()
	crit := collector.Snapshot{Time: now, CPU: collector.CPU{Percent: 95}}
	warn := collector.Snapshot{Time: now, CPU: collector.CPU{Percent: 75}}
	calm := collector.Snapshot{Time: now}

	m.trackAlerts(crit)
	ev := m.openAlerts["cpu"]
	if ev == nil || !ev.Crit {
		t.Fatal("crit crossing must open a critical event")
	}
	m.trackAlerts(warn) // same key, severity de-escalates in place
	if ev.Crit || ev.Text != "CPU 75%" {
		t.Errorf("event not updated in place: %+v", ev)
	}
	m.trackAlerts(calm)
	if _, open := m.openAlerts["cpu"]; open || ev.End.IsZero() {
		t.Error("clearing must close the event")
	}

	for i := 0; i < alertHistory; i++ { // churn past the ring cap
		m.trackAlerts(crit)
		m.trackAlerts(calm)
	}
	if len(m.alerts) != alertHistory {
		t.Errorf("history not capped: %d", len(m.alerts))
	}

	m.width, m.height = 100, 40
	view := stripANSITest(m.alertsOverlayView())
	if strings.Count(view, "\n") > 20 || !strings.Contains(view, "· CPU 95%") || !strings.Contains(view, "events") {
		t.Errorf("overlay should list at most 16 closed events:\n%s", view)
	}
}

func TestViewAndOverlays(t *testing.T) {
	m := sized(t, config.Default(), 100, 30)
	if v := m.View(); v.WindowTitle != "wrongtop" || m.frameCache == "" {
		t.Errorf("pre-sample view: title=%q cached=%v", v.WindowTitle, m.frameCache != "")
	}
	m.alertsMode = true // before any sample: empty history
	out := stripANSITest(m.frame())
	if !strings.Contains(out, "ALERTS") || !strings.Contains(out, "no threshold crossings") {
		t.Errorf("alerts overlay missing:\n%s", out)
	}
	m.alertsMode = false

	m.Update(collector.SnapshotMsg{Snap: fullSnapshot()})
	if v := m.View(); v.WindowTitle != "wrongtop · cpu 34% · mem 62%" {
		t.Errorf("window title = %q", v.WindowTitle)
	}
	first := m.View().Content
	if m.View().Content != first {
		t.Error("View must reuse the cached frame")
	}

	m.helpMode = true
	if out := stripANSITest(m.frame()); !strings.Contains(out, "WRONGTOP HELP") {
		t.Error("help overlay missing")
	}
	m.helpMode = false

	// an ongoing event renders with the live marker and an elapsed time
	m.openAlerts["k"] = &alertEvent{Key: "k", Text: "MEM 99%", Crit: true, Start: time.Now().Add(-90 * time.Second)}
	m.alerts = append(m.alerts, m.openAlerts["k"])
	if out := stripANSITest(m.alertsOverlayView()); !strings.Contains(out, "● MEM 99%") || !strings.Contains(out, "[1m30s]") {
		t.Errorf("ongoing event rendering:\n%s", out)
	}
}

func TestTabBarDegradation(t *testing.T) {
	cfg := config.Default()
	cfg.NerdFonts = true // powerline caps and separators
	m := sized(t, cfg, 120, 30)
	if m.chipSep() == " " {
		t.Error("nerd fonts must render powerline glyphs")
	}
	for i := 0; i < 5; i++ { // five open alerts: three chips + a "+2" overflow
		start := time.Now().Add(-2 * time.Minute)
		ev := &alertEvent{Key: string(rune('a' + i)), Text: "X", Crit: i%2 == 0, Start: start}
		m.openAlerts[ev.Key] = ev
	}
	m.pulse = true
	right := stripANSITest(m.tabBarRight())
	if !strings.Contains(right, "+2") || !strings.Contains(right, "· 2m") {
		t.Errorf("alert chips: %q", right)
	}

	// seven full nerd tabs do not fit in 30 columns even compacted: the
	// compact render is rejected and the full-width bounds are restored
	m.width = 30
	m.tabBarView()
	if got := stripANSITest(m.tabBarLeft(false)); !strings.Contains(got, "PROCESSES") {
		t.Errorf("bounds not restored to full labels: %q", got)
	}

	// three tabs leave room: the alert zone degrades to a bare count chip
	solo := config.Default()
	solo.Modules = config.Modules{}
	solo.NerdFonts = true
	m2 := sized(t, solo, 40, 30)
	for i := 0; i < 5; i++ {
		ev := &alertEvent{Key: string(rune('a' + i)), Text: "X", Start: time.Now()}
		m2.openAlerts[ev.Key] = ev
	}
	bar := stripANSITest(m2.tabBarView())
	if !strings.Contains(bar, "5") || m2.alertZoneStart < 0 {
		t.Errorf("bare alert count missing: %q zone=%d", bar, m2.alertZoneStart)
	}

	m2.width = 8 // nothing fits: the tabs win outright
	m2.tabBarView()
	if m2.alertZoneStart != -1 {
		t.Error("no alert zone when the row is exhausted")
	}
	if _, ok := m.tabAt(9999); ok {
		t.Error("far-right x must not hit a tab")
	}
}

func TestStatusBarAdapts(t *testing.T) {
	cfg := config.Default()
	cfg.NerdFonts = true
	m := New(cfg, "", "test")
	m.width = 180 // roomy: every chip survives
	snap := fullSnapshot()
	m.Update(collector.SnapshotMsg{Snap: snap})

	out := stripANSITest(m.statusBarView())
	if !strings.Contains(out, "65°C") || !strings.Contains(out, "80%") {
		t.Errorf("temperature and battery chips missing: %q", out)
	}
	m.setFlash("hello there")
	if out := stripANSITest(m.statusBarView()); !strings.Contains(out, "hello there") {
		t.Errorf("flash not shown: %q", out)
	}
	m.flashUntil = time.Time{}

	m.width = 40 // narrow: short hints, chips drop
	out = stripANSITest(m.statusBarView())
	if strings.Contains(out, "theme") || !strings.Contains(out, "quit") {
		t.Errorf("narrow bar must use the short hints: %q", out)
	}

	// battery colouring: discharging and low reads red
	m.latest.Battery = &collector.Battery{Percent: 12}
	m.latest.Sensors[0].TempC = 90 // crit
	chips := m.liveChips()
	if len(chips) != 5 || !strings.Contains(stripANSITest(chips[4]), "12%") {
		t.Errorf("chips: %v", chips)
	}
	m.latest.Battery = &collector.Battery{Percent: 55}
	m.latest.Sensors[0].TempC = 20
	m.cfg.NerdFonts = false
	if chips := m.liveChips(); !strings.Contains(stripANSITest(chips[4]), "bat 55%") {
		t.Errorf("plain battery chip: %q", stripANSITest(chips[4]))
	}
	m.latest.Battery = nil
	m.latest.Sensors = nil
	if chips := m.liveChips(); len(chips) != 3 {
		t.Errorf("no sensor/battery → 3 chips, got %d", len(chips))
	}
}

func TestHelpContents(t *testing.T) {
	cfg := config.Default()
	cfg.Keys.Kill = "K"
	m := New(cfg, "", "test")
	if m.killKey() != "k" || m.forceKey() != "K" {
		t.Errorf("kill keys: %s/%s", m.killKey(), m.forceKey())
	}
	if n := len(m.helpSections()); n != 5 {
		t.Errorf("all modules: %d sections, want 5", n)
	}
	out := stripANSITest(m.helpView())
	for _, want := range []string{"GLOBAL", "PROCESSES", "DOCKER", "esc close"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q", want)
		}
	}
	cfg.Modules = config.Modules{}
	if n := len(New(cfg, "", "test").helpSections()); n != 3 {
		t.Errorf("no optional modules: %d sections, want 3", n)
	}
	if got := pad("←", 3); got != "←  " {
		t.Errorf("pad by display width: %q", got)
	}
}

// headless swaps the program I/O so Run executes end to end: the input
// carries a single "q", and the context is a safety net.
func headless(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	orig := programOptions
	programOptions = []tea.ProgramOption{
		tea.WithInput(strings.NewReader("q")),
		tea.WithOutput(io.Discard),
		tea.WithoutSignals(),
		tea.WithoutRenderer(),
		tea.WithContext(ctx),
	}
	t.Cleanup(func() {
		programOptions = orig
		cancel()
	})
}

func TestRunHeadless(t *testing.T) {
	headless(t)
	cfg := config.Default()
	cfg.Modules = config.Modules{}
	if err := Run(cfg, "", "test"); err != nil {
		t.Fatal(err)
	}
}

// streamServer performs the handshake, pushes one snapshot and hangs up,
// so the reader goroutine sees both a snapshot and the end of stream.
func streamServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		var auth remote.Auth
		if remote.ReadFrame(conn, remote.MaxFrame, &auth) != nil {
			return
		}
		_ = remote.WriteFrame(conn, remote.Hello{Protocol: remote.Protocol, RefreshMS: 1000})
		_ = remote.WriteFrame(conn, fullSnapshot())
	}()
	return ln.Addr().String()
}

func TestRunRemoteHeadless(t *testing.T) {
	headless(t)
	addr := streamServer(t)
	stream, err := remote.Dial(context.Background(), addr, "tok", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := RunRemote(config.Default(), "", "test", stream, addr); err != nil {
		t.Fatal(err)
	}
}
