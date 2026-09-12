package docker

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/dockerclient"
	"github.com/wrongstack/wrongtop/internal/theme"
)

func newTestModel() *Model {
	cfg := config.Default()
	return New(cfg, theme.ByName(cfg.Theme))
}

// keyPress builds a printable KeyPressMsg for tests.
func keyPress(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

// testCons covers every container state and health verdict the renderer
// knows about.
func testCons() []dockerclient.Container {
	return []dockerclient.Container{
		{ID: "aaaa1111", Name: "web", Image: "nginx:latest", State: "running",
			Status: "Up 2 hours (healthy)", CPU: 12.5, Mem: 1 << 28, MemPct: 3.3,
			NetRx: 1000, NetTx: 500, BlkR: 100, BlkW: 50},
		{ID: "bbbb2222", Name: "db", Image: "postgres:16", State: "exited",
			Status: "Exited (0) 5 minutes ago"},
		{ID: "cccc3333", Name: "cache", Image: "redis:7", State: "paused",
			Status: "Up 3 hours (unhealthy)"},
		{ID: "dddd4444", Name: "queue", Image: "rabbitmq:3", State: "dead",
			Status: "Dead"},
		{ID: "eeee5555", Name: "init", Image: "busybox:latest", State: "created",
			Status: "Created"},
	}
}

// frame builds one multiplexed docker log-stream frame (stdout, big-endian
// length header) as the daemon would send it.
func frame(payload string) []byte {
	b := make([]byte, 8+len(payload))
	b[0] = 0x01 // stdout stream
	binary.BigEndian.PutUint32(b[4:8], uint32(len(payload)))
	copy(b[8:], payload)
	return b
}

// startFakeDaemon runs an in-process HTTP server answering the two Engine
// API endpoints the tab exercises — the unversioned /_ping probe and the
// container logs stream — and returns a real client pointed at it. No
// docker daemon, no external network: everything stays on 127.0.0.1
// inside the test binary.
func startFakeDaemon(t *testing.T, logs func(http.ResponseWriter, *http.Request)) *dockerclient.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/logs"):
			logs(w, r)
		default: // container inspect: fails, so Logs falls back to tty=false
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("DOCKER_HOST", "tcp://"+strings.TrimPrefix(srv.URL, "http://"))
	cli, err := dockerclient.New()
	if err != nil {
		t.Fatalf("connecting to the fake daemon: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return cli
}

// pumpLogs drains a log stream through Update until it ends, returning
// the collected lines.
func pumpLogs(t *testing.T, m *Model, cmd tea.Cmd) []string {
	t.Helper()
	var got []string
	for range 100 {
		if cmd == nil {
			t.Fatal("log wait loop ended early")
		}
		msg := cmd()
		if _, done := msg.(logDoneMsg); done {
			if cmd = m.Update(msg); cmd != nil {
				t.Fatal("Update re-armed the wait after stream end")
			}
			return got
		}
		line, ok := msg.(logLineMsg)
		if !ok {
			t.Fatalf("unexpected log message %T", msg)
		}
		got = append(got, string(line))
		cmd = m.Update(msg)
	}
	t.Fatal("log stream did not end within 100 messages")
	return nil
}

// TestLogStreamEndTerminatesWaitLoop guards against the end-of-stream
// busy loop: openLogs' reader goroutine closes the stream channel when
// the stream ends (container exited, daemon closed it, or the Logs call
// failed). waitForLog must surface that as a terminal message and Update
// must not re-arm on the closed channel — re-arming busy-loops the
// bubbletea event loop and floods the log buffer with empty lines.
func TestLogStreamEndTerminatesWaitLoop(t *testing.T) {
	m := newTestModel()
	m.logFor = "web"
	stream := make(chan string)
	close(stream) // reader goroutine has returned
	m.logStream = stream

	cmd := m.waitForLog()
	if cmd == nil {
		t.Fatal("waitForLog returned nil while a stream is open")
	}

	// A closed channel delivers instantly, so this cycle is synchronous;
	// a live stream would block inside cmd() instead of spinning here.
	const budget = 100
	terminated := false
	for i := 0; i < budget; i++ {
		msg := cmd()
		cmd = m.Update(msg)
		if cmd == nil {
			terminated = true
			break
		}
	}
	if !terminated {
		t.Fatalf("end-of-stream did not terminate the wait loop within %d updates (m.logs holds %d entries)", budget, len(m.logs))
	}
	if len(m.logs) != 0 {
		t.Fatalf("end-of-stream polluted the log buffer with %d empty line(s)", len(m.logs))
	}
}

// TestLogStreamLinesFlowThenEnd checks the boundary: real lines must
// still flow and re-arm the wait, with clean termination once the buffer
// is drained.
func TestLogStreamLinesFlowThenEnd(t *testing.T) {
	m := newTestModel()
	m.logFor = "web"
	stream := make(chan string, 2)
	stream <- "hello"
	stream <- "world"
	close(stream)
	m.logStream = stream

	cmd := m.waitForLog()
	for _, want := range []string{"hello", "world"} {
		msg := cmd()
		line, ok := msg.(logLineMsg)
		if !ok {
			t.Fatalf("expected logLineMsg %q, got %T", want, msg)
		}
		if string(line) != want {
			t.Fatalf("got line %q, want %q", line, want)
		}
		if cmd = m.Update(msg); cmd == nil {
			t.Fatalf("Update stopped re-arming while lines remain (waiting for %q)", want)
		}
	}
	msg := cmd() // drained buffer + closed channel → terminal
	if cmd = m.Update(msg); cmd != nil {
		t.Fatalf("Update re-armed after stream end (%T)", msg)
	}
	if len(m.logs) != 2 {
		t.Fatalf("logs = %v, want [hello world]", m.logs)
	}
}

// TestCloseLogsStopsWaiting covers the esc path: after closeLogs resets
// the view, a terminal message from the old stream must not re-arm a
// wait on the nil stream, which would park a goroutine forever.
func TestCloseLogsStopsWaiting(t *testing.T) {
	m := newTestModel()
	m.logFor = "web"
	stream := make(chan string)
	close(stream)
	m.logStream = stream

	m.closeLogs()
	if m.logFor != "" || m.logStream != nil {
		t.Fatal("closeLogs did not reset the log view state")
	}
	if cmd := m.Update(logDoneMsg{}); cmd != nil {
		t.Fatal("Update re-armed the log wait after closeLogs")
	}
	if len(m.logs) != 0 {
		t.Fatalf("closeLogs left %d entries in the log buffer", len(m.logs))
	}
}

// TestTabBasics exercises the ui.Tab contract: title, theme swap, size,
// and the visible/hidden rebuild gating.
func TestTabBasics(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))

	if title := m.Title(); !strings.Contains(title, "DOCKER") {
		t.Fatalf("Title() = %q, want it to mention DOCKER", title)
	}

	other := theme.ByName("dracula")
	m.SetTheme(other)
	if m.th != other {
		t.Fatal("SetTheme did not swap the theme")
	}

	m.SetSize(80, 24)
	if m.width != 80 || m.height != 24 {
		t.Fatalf("size = %dx%d, want 80x24", m.width, m.height)
	}
	if m.table.Height() <= 0 {
		t.Fatalf("list mode table height = %d, want a visible viewport", m.table.Height())
	}

	m.Update(dockerclient.UpdateMsg{Containers: testCons()})
	if got := len(m.table.Rows()); got != len(testCons()) {
		t.Fatalf("visible update rebuilt %d rows, want %d", got, len(testCons()))
	}

	// hidden tabs keep the data but skip the row rebuild
	m.SetVisible(false)
	if m.visible {
		t.Fatal("SetVisible(false) left the tab visible")
	}
	shrunk := testCons()[:2]
	m.Update(dockerclient.UpdateMsg{Containers: shrunk})
	if len(m.cons) != 2 {
		t.Fatalf("hidden update did not store the containers: %d", len(m.cons))
	}
	if got := len(m.table.Rows()); got != len(testCons()) {
		t.Fatalf("hidden update rebuilt rows (%d), want the stale %d", got, len(testCons()))
	}

	// activation catches up
	m.SetVisible(true)
	if got := len(m.table.Rows()); got != 2 {
		t.Fatalf("SetVisible(true) rebuilt %d rows, want 2", got)
	}

	// unrelated messages are ignored
	if cmd := m.Update(tea.QuitMsg{}); cmd != nil {
		t.Fatal("Update returned a command for an unhandled message")
	}
}

// TestLayoutsAndColumns checks that every terminal-width band renders the
// right column set and that rows follow the column shape.
func TestLayoutsAndColumns(t *testing.T) {
	cases := []struct {
		width    int
		wantCols int
	}{
		{70, 6},   // NAME IMAGE STATE CPU% MEM MEM%
		{100, 8},  // + ACTIVITY NET
		{130, 9},  // + STATUS
		{160, 10}, // + BLOCK
	}
	for _, tc := range cases {
		m := newTestModel()
		m.SetSize(tc.width, 24)
		if got := len(m.table.Columns()); got != tc.wantCols {
			t.Errorf("width %d: %d columns, want %d", tc.width, got, tc.wantCols)
		}
		m.Update(dockerclient.UpdateMsg{Containers: testCons()})
		for i, row := range m.table.Rows() {
			if len(row) != tc.wantCols {
				t.Errorf("width %d: row %d has %d cells, want %d", tc.width, i, len(row), tc.wantCols)
			}
		}
	}

	// layout predicates per band
	l := dockerLayoutFor(160)
	if !l.net || !l.block || !l.status || !l.bar || l.imageW != 18 {
		t.Errorf("dockerLayoutFor(160) = %+v", l)
	}
	l = dockerLayoutFor(150) // slack below the +8 threshold clamps IMAGE
	if !l.block || l.imageW != 12 {
		t.Errorf("dockerLayoutFor(150) = %+v", l)
	}
	l = dockerLayoutFor(118)
	if !l.net || !l.status || !l.bar || l.block {
		t.Errorf("dockerLayoutFor(118) = %+v", l)
	}
	l = dockerLayoutFor(94)
	if !l.net || !l.bar || l.status || l.block {
		t.Errorf("dockerLayoutFor(94) = %+v", l)
	}
	l = dockerLayoutFor(60)
	if l.net || l.block || l.status || l.bar || l.imageW != 12 {
		t.Errorf("dockerLayoutFor(60) = %+v", l)
	}
}

// TestRatesAcrossSnapshots pins the diffing of cumulative counters, the
// zeroed first sighting and the restart (backwards counter) case.
func TestRatesAcrossSnapshots(t *testing.T) {
	m := newTestModel()
	m.SetSize(160, 24)

	m.Update(dockerclient.UpdateMsg{Containers: []dockerclient.Container{
		{ID: "aaa", NetRx: 100, NetTx: 200, BlkR: 10, BlkW: 20},
	}})
	if r := m.rates["aaa"]; r != [4]float64{} {
		t.Fatalf("first sighting must yield zero rates, got %v", r)
	}

	time.Sleep(2 * time.Millisecond) // guarantee dt > 0 between polls
	m.Update(dockerclient.UpdateMsg{Containers: []dockerclient.Container{
		{ID: "aaa", NetRx: 100 + 2000, NetTx: 200, BlkR: 10 + 100, BlkW: 25},
	}})
	r := m.rates["aaa"]
	if r[0] <= 0 || r[1] != 0 || r[2] <= 0 || r[3] <= 0 {
		t.Fatalf("rates after growth = %v, want positive rx/block and zero tx", r)
	}

	// a counter going backwards (container restart) yields zero, not a
	// huge wrapped value
	time.Sleep(2 * time.Millisecond)
	m.Update(dockerclient.UpdateMsg{Containers: []dockerclient.Container{
		{ID: "aaa", NetRx: 1, NetTx: 1, BlkR: 1, BlkW: 1},
	}})
	if r = m.rates["aaa"]; r != [4]float64{} {
		t.Fatalf("backwards counters must yield zeros, got %v", r)
	}

	// a newly seen container starts at zero even mid-stream
	time.Sleep(2 * time.Millisecond)
	m.Update(dockerclient.UpdateMsg{Containers: []dockerclient.Container{
		{ID: "aaa", NetRx: 1, NetTx: 1, BlkR: 1, BlkW: 1},
		{ID: "bbb", NetRx: 5, NetTx: 5, BlkR: 5, BlkW: 5},
	}})
	if r := m.rates["bbb"]; r != [4]float64{} {
		t.Fatalf("new container must start at zero rates, got %v", r)
	}

	// the rate columns render into wide-terminal rows: cells 7/8 are the
	// NET and BLOCK columns at width 160
	rows := m.table.Rows()
	if !strings.Contains(rows[0][7], " / ") || !strings.Contains(rows[0][8], " / ") {
		t.Errorf("rate cells should render ↓/↑ pairs: net %q block %q", rows[0][7], rows[0][8])
	}
}

// TestShortRate covers the dense-column rate rendering.
func TestShortRate(t *testing.T) {
	cases := map[string]struct {
		bps  float64
		want string
	}{
		"zero":  {0, "0 B"},
		"bytes": {999, "999 B"},
		"kilo":  {1500, "1.5 Kb"},
		"mega":  {2.5e6, "2.5 Mb"},
	}
	for name, tc := range cases {
		if got := shortRate(tc.bps); got != tc.want {
			t.Errorf("%s: shortRate(%v) = %q, want %q", name, tc.bps, got, tc.want)
		}
	}
}

// TestViewWithoutDaemon covers the unreachable-daemon notice, both with
// and without a recorded error.
func TestViewWithoutDaemon(t *testing.T) {
	m := newTestModel()
	if v := m.View(); v != "" {
		t.Fatalf("zero-sized view = %q, want empty", v)
	}

	m.SetSize(80, 24)
	v := m.View()
	for _, want := range []string{"Docker daemon not reachable.", "start Docker Desktop"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}

	m.err = errors.New("cannot connect to the Docker daemon")
	if v := m.View(); !strings.Contains(v, "cannot connect to the Docker daemon") {
		t.Errorf("view should surface the poll error:\n%s", v)
	}
}

// TestViewListAndEmpty covers the daemon-reachable views: populated table
// and the no-containers placeholder.
func TestViewListAndEmpty(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(120, 30)
	m.client = &dockerclient.Client{} // reachable, zero-value client suffices

	if v := m.View(); !strings.Contains(v, "no containers") {
		t.Fatalf("empty daemon view missing the placeholder:\n%s", v)
	}

	m.Update(dockerclient.UpdateMsg{Client: m.client, Containers: testCons()})
	v := m.View()
	for _, want := range []string{"5 containers", "web", "nginx", "enter", "logs", "start", "stop", "restart"} {
		if !strings.Contains(v, want) {
			t.Errorf("list view missing %q", want)
		}
	}

	m.status = "restarted web"
	if v := m.View(); !strings.Contains(v, "restarted web") {
		t.Errorf("list view should show the action status:\n%s", v)
	}
}

// TestKeysWithoutClient makes sure keys are inert while no daemon is
// connected — no dialogs, no closures, no panics.
func TestKeysWithoutClient(t *testing.T) {
	m := newTestModel()
	m.SetSize(100, 24)

	for _, key := range []string{"s", "t", "r"} {
		if cmd := m.Update(keyPress(key)); cmd != nil {
			t.Errorf("key %q returned a command without a client", key)
		}
	}
	if cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Error("enter opened logs without a client")
	}
	if m.status != "" {
		t.Errorf("status changed without a client: %q", m.status)
	}

	// unhandled keys fall through to the table; with no rows the cursor
	// just stays parked out of range
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.table.Cursor(); got != -1 {
		t.Errorf("cursor moved on an empty table: %d, want -1", got)
	}
}

// TestLifecycleActions covers the s/t/r action builders. The returned
// closures are built but never executed: against a zero-value client they
// would dereference a nil daemon connection.
func TestLifecycleActions(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(120, 30)
	m.client = &dockerclient.Client{}
	m.Update(dockerclient.UpdateMsg{Client: m.client, Containers: testCons()})

	// a fresh rebuild leaves the cursor at the bubbles out-of-range marker
	// until the user navigates; move onto the first row first
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.table.Cursor(); got != 0 {
		t.Fatalf("down arrow put the cursor at %d, want 0", got)
	}

	for _, key := range []string{"s", "t", "r"} {
		if cmd := m.Update(keyPress(key)); cmd == nil {
			t.Errorf("key %q did not build an action command", key)
		}
	}

	// action with no container selected is a no-op
	empty := New(config.Default(), theme.ByName(config.Default().Theme))
	empty.SetSize(120, 30)
	if cmd := empty.action("start", func(context.Context, string) error { return nil }); cmd != nil {
		t.Error("action with no rows returned a command")
	}

	// the built closure wraps the callback result as actionDoneMsg
	called := false
	cmd := m.action("restart", func(context.Context, string) error {
		called = true
		return errors.New("boom")
	})
	msg := cmd()
	done, ok := msg.(actionDoneMsg)
	if !ok {
		t.Fatalf("action closure returned %T, want actionDoneMsg", msg)
	}
	if !called || done.label != "restart" || done.err == nil {
		t.Fatalf("actionDoneMsg = %+v (called=%v), want executed restart failure", done, called)
	}

	cmd = m.action("start", func(context.Context, string) error { return nil })
	done = cmd().(actionDoneMsg)
	if done.label != "start" || done.err != nil {
		t.Fatalf("successful action = %+v, want label start with nil error", done)
	}
}

// TestOpenLogsNotRunning covers enter on exited or missing containers.
func TestOpenLogsNotRunning(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(120, 30)
	m.client = &dockerclient.Client{}
	m.Update(dockerclient.UpdateMsg{Client: m.client, Containers: testCons()})

	// move the cursor onto the exited db container (down once lands on
	// the running web container)
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	if cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Error("enter on an exited container returned a command")
	}
	if m.status != "logs: container is not running" {
		t.Fatalf("status = %q, want the not-running notice", m.status)
	}
	if m.logFor != "" {
		t.Fatalf("log view opened for %q", m.logFor)
	}

	// and with nothing on the daemon at all
	empty := New(config.Default(), theme.ByName(config.Default().Theme))
	empty.SetSize(120, 30)
	empty.client = &dockerclient.Client{}
	if cmd := empty.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Error("enter on an empty list returned a command")
	}
}

// TestOpenLogsStreamsLines drives the full log pipeline against the fake
// daemon: open, two demultiplexed lines, clean end of stream, esc close.
func TestOpenLogsStreamsLines(t *testing.T) {
	cli := startFakeDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.docker.multiplexed-stream")
		_, _ = w.Write(frame("hello\n"))
		_, _ = w.Write(frame("world\n"))
	})

	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(120, 30)
	m.Update(dockerclient.UpdateMsg{Client: cli, Containers: []dockerclient.Container{
		{ID: "abc123", Name: "web", State: "running"},
	}})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // move the cursor onto the row

	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on a running container did not start the log wait")
	}
	if m.logFor != "web" {
		t.Fatalf("log view is for %q, want web", m.logFor)
	}
	if m.table.Height() > 0 {
		t.Fatalf("log mode should collapse the table, height = %d", m.table.Height())
	}

	got := pumpLogs(t, m, cmd)
	if len(got) != 2 || got[0] != "hello" || got[1] != "world" {
		t.Fatalf("pumped %q, want [hello world]", got)
	}
	if len(m.logs) != 2 {
		t.Fatalf("buffer holds %v", m.logs)
	}

	v := m.View()
	for _, want := range []string{"LOGS · web", "2 lines", "hello", "world", "esc", "back"} {
		if !strings.Contains(v, want) {
			t.Errorf("log view missing %q", want)
		}
	}

	// a resize while logs are open keeps the table collapsed
	m.SetSize(100, 30)
	if m.table.Height() > 0 {
		t.Errorf("resize in log mode should keep the table collapsed, height = %d", m.table.Height())
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.logFor != "" || m.logStream != nil || len(m.logs) != 0 {
		t.Fatal("esc did not close the log view")
	}
	if m.table.Height() <= 0 {
		t.Errorf("closing logs should restore the table, height = %d", m.table.Height())
	}

	// enter also closes the log view
	m.logFor = "web"
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.logFor != "" {
		t.Fatal("enter did not close the log view")
	}
}

// TestOpenLogsDaemonError covers the reader goroutine's error path: the
// daemon rejects the logs request and the error surfaces as a log line.
func TestOpenLogsDaemonError(t *testing.T) {
	cli := startFakeDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "logs unavailable", http.StatusInternalServerError)
	})

	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(120, 30)
	m.Update(dockerclient.UpdateMsg{Client: cli, Containers: []dockerclient.Container{
		{ID: "abc123", Name: "web", State: "running"},
	}})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter did not start the log wait")
	}
	got := pumpLogs(t, m, cmd)
	if len(got) != 1 || !strings.HasPrefix(got[0], "logs: ") {
		t.Fatalf("pumped %q, want one line starting with \"logs: \"", got)
	}
	if len(m.logs) != 1 || !strings.HasPrefix(m.logs[0], "logs: ") {
		t.Fatalf("buffer holds %q", m.logs)
	}
}

// TestOpenLogsCancelStopsStream proves the ctx-cancel escape hatch of the
// reader select: with the stream channel buffer full, cancelling the
// context unblocks the goroutine via the ctx.Done case instead of leaving
// it parked forever. The daemon sends 257 lines: 256 fill the buffer, the
// 257th send blocks until the cancel.
func TestOpenLogsCancelStopsStream(t *testing.T) {
	const total = 257
	cli := startFakeDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.docker.multiplexed-stream")
		for i := 0; i < total; i++ {
			_, _ = w.Write(frame(fmt.Sprintf("line-%03d\n", i)))
		}
	})

	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(120, 30)
	m.Update(dockerclient.UpdateMsg{Client: cli, Containers: []dockerclient.Container{
		{ID: "abc123", Name: "web", State: "running"},
	}})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // deliberately not pumped
	if cmd == nil {
		t.Fatal("enter did not start the log wait")
	}
	ch := m.logStream

	// wait (bounded) until the reader goroutine fills the buffer and
	// parks on the 257th send
	deadline := time.Now().Add(2 * time.Second)
	for len(ch) < 256 {
		if time.Now().After(deadline) {
			t.Fatalf("log buffer never filled: %d/256 lines", len(ch))
		}
		time.Sleep(time.Millisecond)
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // closeLogs cancels the ctx
	if m.logFor != "" {
		t.Fatal("esc did not leave log mode")
	}

	// draining returns exactly the buffered lines and then the channel
	// close — proof the reader goroutine took the ctx.Done branch
	n := 0
	for line := range ch {
		if want := fmt.Sprintf("line-%03d", n); line != want {
			t.Fatalf("line %d = %q, want %q", n, line, want)
		}
		n++
	}
	if n != 256 {
		t.Fatalf("drained %d lines, want the 256 buffered ones", n)
	}
}

// TestLogBufferCap verifies the in-memory cap: line 1001+ must evict the
// oldest entries.
func TestLogBufferCap(t *testing.T) {
	m := newTestModel()
	m.SetSize(120, 30)
	m.logFor = "web"

	const total = dockerLogLines + 1
	ch := make(chan string, total)
	for i := 0; i < total; i++ {
		ch <- fmt.Sprintf("line-%d", i)
	}
	close(ch)
	m.logStream = ch

	cmd := m.waitForLog()
	for i := 0; i < total; i++ {
		msg := cmd()
		if cmd = m.Update(msg); cmd == nil && i < total-1 {
			t.Fatalf("wait disarmed after %d/%d lines", i+1, total)
		}
	}
	if len(m.logs) != dockerLogLines {
		t.Fatalf("buffer holds %d lines, want %d", len(m.logs), dockerLogLines)
	}
	if m.logs[0] != "line-1" || m.logs[dockerLogLines-1] != fmt.Sprintf("line-%d", dockerLogLines) {
		t.Fatalf("cap evicted the wrong end: first=%q last=%q", m.logs[0], m.logs[dockerLogLines-1])
	}
}

// TestLogScrolling covers the log pane's scrollback clamps, the scrolled
// header chip, long-line clipping and inert clicks.
func TestLogScrolling(t *testing.T) {
	m := newTestModel()
	m.SetSize(60, 24) // scrollbackMax = 30 - (24-3) = 9
	m.logFor = "web"
	m.logs = make([]string, 0, 31)
	m.logs = append(m.logs, strings.Repeat("a", 200)) // wider than the pane
	m.logs = append(m.logs, "fatal: nope", "panic!!", "error x", "err: y", "warn z", "debug a", "trace b", "plain")
	for len(m.logs) < 30 {
		m.logs = append(m.logs, "filler")
	}

	// wheel scrolling clamps at the top of the buffer
	for i := 0; i < 3; i++ {
		m.Update(tea.MouseWheelMsg{Y: 1, Button: tea.MouseWheelUp})
	}
	if m.logScroll != m.scrollbackMax() || m.logScroll != 9 {
		t.Fatalf("logScroll = %d, want it clamped at scrollbackMax 9", m.logScroll)
	}
	if v := m.View(); !strings.Contains(v, "↑9") {
		t.Error("scrolled log view missing the scrollback chip")
	}
	// long lines are clipped, not soft-wrapped
	if v := m.View(); !strings.Contains(v, strings.Repeat("a", 50)) {
		t.Error("long log line missing from the view")
	}

	// and at the bottom
	for i := 0; i < 5; i++ {
		m.Update(tea.MouseWheelMsg{Y: 1, Button: tea.MouseWheelDown})
	}
	if m.logScroll != 0 {
		t.Fatalf("logScroll = %d after scrolling down, want 0", m.logScroll)
	}
	m.Update(tea.MouseWheelMsg{Y: 1, Button: tea.MouseWheelDown}) // stays at 0
	if m.logScroll != 0 {
		t.Fatalf("logScroll = %d below the tail, want 0", m.logScroll)
	}
	if v := m.View(); strings.Contains(v, "↑") {
		t.Error("unscrolled log view shows a scrollback chip")
	}

	// clicks do nothing in log mode
	m.Update(tea.MouseClickMsg{Y: 5, Button: tea.MouseLeft})
	if m.logScroll != 0 {
		t.Errorf("click changed the scroll: %d", m.logScroll)
	}
}

// TestSeverityStyle pins the level-word heuristic.
func TestSeverityStyle(t *testing.T) {
	m := newTestModel()
	cases := map[string]string{
		"fatal: boom":  "crit",
		"panic: boom":  "crit",
		"error: boom":  "crit",
		"err: boom":    "crit",
		"FATAL SHIFT":  "crit",
		"warn: low":    "warn",
		"debug detail": "muted",
		"trace on":     "muted",
		"hello world":  "plain",
	}
	for line, want := range cases {
		got := m.severityStyle(line).Render("x")
		var wantRender string
		switch want {
		case "crit":
			wantRender = m.th.Styles.Crit.Render("x")
		case "warn":
			wantRender = m.th.Styles.Warn.Render("x")
		case "muted":
			wantRender = m.th.Styles.Muted.Render("x")
		default:
			wantRender = "x"
		}
		if got != wantRender {
			t.Errorf("severityStyle(%q) rendered %q, want the %s style", line, got, want)
		}
	}
}

// TestStatusCell pins the health-verdict coloring of the STATUS column.
func TestStatusCell(t *testing.T) {
	m := newTestModel()
	cases := map[string]string{
		"Up 3 hours (unhealthy)": "crit",
		"Dead":                   "crit",
		"Restarting (1) 5s":      "warn",
		"Starting":               "warn",
		"Up 2 hours (healthy)":   "ok",
		"Healthy":                "ok",
		"Created":                "muted",
	}
	for status, want := range cases {
		got := m.statusCell(status).Render("x")
		var wantRender string
		switch want {
		case "crit":
			wantRender = m.th.Styles.Crit.Render("x")
		case "warn":
			wantRender = m.th.Styles.Warn.Render("x")
		case "ok":
			wantRender = m.th.Styles.OK.Render("x")
		default:
			wantRender = m.th.Styles.Muted.Render("x")
		}
		if got != wantRender {
			t.Errorf("statusCell(%q) rendered %q, want the %s style", status, got, want)
		}
	}
}

// TestMouseListMode covers wheel scrolling and row clicking while the
// container list is shown.
func TestMouseListMode(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(120, 30)
	m.client = &dockerclient.Client{}
	m.Update(dockerclient.UpdateMsg{Client: m.client, Containers: testCons()})

	m.Update(tea.MouseWheelMsg{Y: 1, Button: tea.MouseWheelDown})
	if got := m.table.Cursor(); got != 2 {
		t.Fatalf("wheel down moved the cursor to %d, want 2 (from the -1 out-of-range marker)", got)
	}
	m.Update(tea.MouseWheelMsg{Y: 1, Button: tea.MouseWheelUp})
	if got := m.table.Cursor(); got != 0 {
		t.Fatalf("wheel up moved the cursor to %d, want 0", got)
	}

	// clicks map window rows to table rows: park on row 2 first so the
	// click has to move the cursor to prove it landed
	m.Update(tea.MouseWheelMsg{Y: 1, Button: tea.MouseWheelDown})
	if got := m.table.Cursor(); got != 3 {
		t.Fatalf("cursor at %d before the click, want 3", got)
	}
	m.Update(tea.MouseClickMsg{Y: 3, Button: tea.MouseLeft})
	if got := m.table.Cursor(); got != 0 {
		t.Fatalf("click on the first row moved the cursor to %d, want 0", got)
	}
	m.Update(tea.MouseClickMsg{Y: 4, Button: tea.MouseLeft})
	if got := m.table.Cursor(); got != 1 {
		t.Fatalf("click on the second row moved the cursor to %d, want 1", got)
	}
	m.Update(tea.MouseClickMsg{Y: 4, Button: tea.MouseRight})
	if got := m.table.Cursor(); got != 1 {
		t.Fatalf("right click moved the cursor to %d, want 1", got)
	}
	m.Update(tea.MouseClickMsg{Y: 200, Button: tea.MouseLeft})
	if got := m.table.Cursor(); got != 1 {
		t.Fatalf("click past the rows moved the cursor to %d, want 1", got)
	}

	// without a client, clicks are ignored entirely
	noClient := New(config.Default(), theme.ByName(config.Default().Theme))
	noClient.SetSize(120, 30)
	noClient.Update(dockerclient.UpdateMsg{Containers: testCons()})
	before := noClient.table.Cursor() // the out-of-range marker
	noClient.Update(tea.MouseClickMsg{Y: 3, Button: tea.MouseLeft})
	if got := noClient.table.Cursor(); got != before {
		t.Fatalf("click without a daemon moved the cursor from %d to %d", before, got)
	}
}

// Action results (actionDoneMsg) are the docker tab's only immediate
// feedback for start/stop/restart: success and daemon errors must both
// surface in the head line via m.status, and a later success must
// replace an earlier failure.
func TestActionDoneSetsStatus(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(120, 30)
	m.Update(dockerclient.UpdateMsg{Client: &dockerclient.Client{}, Containers: []dockerclient.Container{{
		ID: "abc123def456", Name: "web", State: "running",
	}}})

	m.Update(actionDoneMsg{label: "restart", err: errors.New("boom")})
	if head := strings.SplitN(m.View(), "\n", 2)[0]; !strings.Contains(head, "restart: boom") {
		t.Errorf("failed action feedback missing from the head: %q", head)
	}

	m.Update(actionDoneMsg{label: "restart"})
	if head := strings.SplitN(m.View(), "\n", 2)[0]; !strings.Contains(head, "restart: done") {
		t.Errorf("success feedback missing from the head: %q", head)
	}
	if head := strings.SplitN(m.View(), "\n", 2)[0]; strings.Contains(head, "boom") {
		t.Errorf("stale failure feedback survived the success result: %q", head)
	}
}
