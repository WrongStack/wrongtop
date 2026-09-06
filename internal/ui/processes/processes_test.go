package processes

import (
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/procs"
	"github.com/wrongstack/wrongtop/internal/theme"
)

// ghostPID is above every platform's pid_max (99998 on darwin, 2^22-1 on
// linux), so no process can ever own it. Signal paths driven with this
// pid fail inside gopsutil's existence check — before any signal is
// actually sent.
const ghostPID = int32(2147483000)

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

func newTestModel(t *testing.T, cfg *config.Config) *Model {
	t.Helper()
	if cfg == nil {
		cfg = config.Default()
	}
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(120, 30)
	return m
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

// TestTabBasics exercises the ui.Tab contract: title, theme swap, and the
// visible/hidden rebuild gating.
func TestTabBasics(t *testing.T) {
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(120, 30)

	if title := m.Title(); !strings.Contains(title, "PROCESSES") {
		t.Fatalf("Title() = %q, want it to mention PROCESSES", title)
	}

	other := theme.ByName("nord")
	m.SetTheme(other)
	if m.th != other {
		t.Fatal("SetTheme did not swap the theme")
	}

	three := collector.Snapshot{Time: time.Now(), Procs: []collector.Proc{
		{PID: 1, Name: "a"}, {PID: 2, Name: "b"}, {PID: 3, Name: "c"},
	}}
	m.Update(collector.SnapshotMsg{Snap: three})
	if got := len(m.table.Rows()); got != 3 {
		t.Fatalf("visible update rebuilt %d rows, want 3", got)
	}

	// hidden tabs keep the snapshot but skip the row rebuild
	m.SetVisible(false)
	if m.visible {
		t.Fatal("SetVisible(false) left the tab visible")
	}
	seven := collector.Snapshot{Time: time.Now(), Procs: make([]collector.Proc, 7)}
	for i := range seven.Procs {
		seven.Procs[i] = collector.Proc{PID: int32(i + 10), Name: "p"}
	}
	m.Update(collector.SnapshotMsg{Snap: seven})
	if len(m.procs) != 7 {
		t.Fatalf("hidden update did not store the snapshot: %d procs", len(m.procs))
	}
	if got := len(m.table.Rows()); got != 3 {
		t.Fatalf("hidden update rebuilt rows (%d), want the stale 3", got)
	}

	// activation catches up
	m.SetVisible(true)
	if got := len(m.table.Rows()); got != 7 {
		t.Fatalf("SetVisible(true) rebuilt %d rows, want 7", got)
	}

	// unrelated messages are ignored
	if cmd := m.Update(tea.QuitMsg{}); cmd != nil {
		t.Fatal("Update returned a command for an unhandled message")
	}
}

// TestMouseWheelAndClick covers wheel navigation and click-to-select,
// including the guards that ignore clicks while the filter or the signal
// prompt is open.
func TestMouseWheelAndClick(t *testing.T) {
	m := newTestModel(t, nil)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{Time: time.Now(),
		Procs: procs10()}})
	if m.table.Cursor() != 0 {
		t.Fatalf("cursor at %d after the first snapshot, want 0", m.table.Cursor())
	}

	m.Update(tea.MouseWheelMsg{Y: 1, Button: tea.MouseWheelDown})
	if got := m.table.Cursor(); got != 3 {
		t.Fatalf("wheel down moved the cursor to %d, want 3", got)
	}
	m.Update(tea.MouseWheelMsg{Y: 1, Button: tea.MouseWheelDown})
	if got := m.table.Cursor(); got != 6 {
		t.Fatalf("wheel down moved the cursor to %d, want 6", got)
	}
	m.Update(tea.MouseWheelMsg{Y: 1, Button: tea.MouseWheelUp})
	if got := m.table.Cursor(); got != 3 {
		t.Fatalf("wheel up moved the cursor to %d, want 3", got)
	}
	m.Update(tea.MouseWheelMsg{Y: 1, Button: tea.MouseWheelUp})
	m.Update(tea.MouseWheelMsg{Y: 1, Button: tea.MouseWheelUp})
	if got := m.table.Cursor(); got != 0 {
		t.Fatalf("wheel up past the top moved the cursor to %d, want 0", got)
	}

	// clicks map window rows to data rows
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

	// clicks are ignored while the filter is being edited
	m.Update(keyPress("/"))
	if !m.editing {
		t.Fatal("filter did not open")
	}
	m.Update(tea.MouseClickMsg{Y: 5, Button: tea.MouseLeft})
	if got := m.table.Cursor(); got != 1 {
		t.Fatalf("click while editing moved the cursor to %d, want 1", got)
	}
	if !m.editing {
		t.Fatal("click closed the filter")
	}
	m.Update(escKey())

	// ...and while the signal prompt is open
	m.Update(keyPress("k"))
	if m.confirm == nil {
		t.Fatal("kill key did not open the prompt")
	}
	m.Update(tea.MouseClickMsg{Y: 5, Button: tea.MouseLeft})
	if m.confirm == nil {
		t.Fatal("click dismissed the signal prompt")
	}
	if got := m.table.Cursor(); got != 1 {
		t.Fatalf("click with the prompt open moved the cursor to %d, want 1", got)
	}
	m.Update(escKey())
}

// TestCmdlineMsgRouting checks that a lookup result lands only in the
// detail box that requested it.
func TestCmdlineMsgRouting(t *testing.T) {
	m := newTestModel(t, nil)
	m.Update(collector.SnapshotMsg{Snap: testSnapshot()})

	m.detail = &procDetail{proc: collector.Proc{PID: 42}}
	m.Update(cmdlineMsg{pid: 42, text: "/bin/testproc -x"})
	if m.detail.cmdline != "/bin/testproc -x" {
		t.Fatalf("cmdline = %q, want the lookup result", m.detail.cmdline)
	}

	m.Update(cmdlineMsg{pid: 99, text: "stale"})
	if m.detail.cmdline != "/bin/testproc -x" {
		t.Fatalf("cmdline = %q, a foreign pid must not overwrite it", m.detail.cmdline)
	}

	m.detail = nil // late reply after the box closed: must not panic
	m.Update(cmdlineMsg{pid: 42, text: "late"})
}

// TestOpenDetail covers enter/esc around the detail box: the on-demand
// cmdline lookup in normal mode, no lookup in read-only mode, and enter
// with nothing selected.
func TestOpenDetail(t *testing.T) {
	m := newTestModel(t, nil)
	// the fixture process does not exist (nothing can own ghostPID), so
	// the single on-demand lookup below fails fast without spawning ps
	snap := collector.Snapshot{Time: time.Now(),
		Procs: []collector.Proc{{PID: ghostPID, Name: "ghost"}}}
	m.Update(collector.SnapshotMsg{Snap: snap})

	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.detail == nil || m.detail.proc.PID != ghostPID {
		t.Fatalf("enter did not open the detail box: %+v", m.detail)
	}
	if cmd == nil {
		t.Fatal("enter in writable mode did not start the cmdline lookup")
	}
	msg, ok := cmd().(cmdlineMsg) // executed once, on the ghost pid
	if !ok || msg.pid != ghostPID {
		t.Fatalf("lookup returned %+v, want a cmdlineMsg for the selected pid", msg)
	}
	m.Update(msg) // the reply lands in the open box
	if m.detail.cmdline != msg.text {
		t.Fatalf("cmdline = %q, want %q", m.detail.cmdline, msg.text)
	}

	m.Update(escKey())
	if m.detail != nil {
		t.Fatal("esc did not close the detail box")
	}

	// enter with nothing selected (empty table, cursor out of range) is a
	// no-op
	empty := newTestModel(t, nil)
	if cmd := empty.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("enter with no selection returned a lookup command")
	}
	if empty.detail != nil {
		t.Fatal("enter with no selection opened the detail box")
	}

	// read-only views skip the local cmdline lookup entirely
	ro := config.Default()
	ro.ReadOnly = true
	m2 := newTestModel(t, ro)
	m2.Update(collector.SnapshotMsg{Snap: testSnapshot()})
	if cmd := m2.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("read-only mode started a cmdline lookup")
	}
	if m2.detail == nil || m2.detail.proc.PID != 42 {
		t.Fatal("read-only mode did not open the detail box")
	}
}

// TestExpandKeyGuards covers the no-op branches of the collapse/expand
// keys: flat mode, leaf rows and an empty table.
func TestExpandKeyGuards(t *testing.T) {
	m := newTestModel(t, nil)
	m.Update(collector.SnapshotMsg{Snap: testSnapshot()})

	// flat mode: left/right do nothing at all
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if len(m.collapsed) != 0 {
		t.Fatalf("flat mode collapsed something: %v", m.collapsed)
	}
	if len(m.rows) != 1 {
		t.Fatalf("flat mode rows changed: %d", len(m.rows))
	}

	// tree mode, empty table: cursor out of range, still nothing
	m.Update(keyPress("t"))
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{Time: time.Now()}})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if len(m.collapsed) != 0 {
		t.Fatalf("empty tree collapsed something: %v", m.collapsed)
	}

	// tree mode, leaf row: no children to collapse
	leaf := collector.Snapshot{Time: time.Now(),
		Procs: []collector.Proc{{PID: 1, Name: "lonely"}}}
	m.Update(collector.SnapshotMsg{Snap: leaf})
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if len(m.collapsed) != 0 {
		t.Fatalf("leaf row collapsed: %v", m.collapsed)
	}
}

// TestFilterEditing covers the live filter: typing rebuilds, enter keeps
// the filter, esc clears it, and non-text keys leave it untouched.
func TestFilterEditing(t *testing.T) {
	m := newTestModel(t, nil)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{Time: time.Now(),
		Procs: []collector.Proc{
			{PID: 42, Name: "testproc"},
			{PID: 7, Name: "launchd"},
		}}})

	m.Update(keyPress("/"))
	if !m.editing {
		t.Fatal("filter did not open")
	}
	for _, r := range "test" {
		m.Update(keyPress(string(r)))
	}
	if m.filter != "test" {
		t.Fatalf("filter = %q, want test", m.filter)
	}
	if len(m.rows) != 1 || m.rows[0].Proc.Name != "testproc" {
		t.Fatalf("filter did not narrow the rows: %+v", m.rows)
	}

	// a key that edits nothing must not rebuild with a new filter
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.filter != "test" || !m.editing {
		t.Fatalf("arrow key changed the filter state: %q editing=%v", m.filter, m.editing)
	}

	// enter commits the filter and leaves edit mode
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.editing {
		t.Fatal("enter did not leave edit mode")
	}
	if m.filter != "test" || len(m.rows) != 1 {
		t.Fatalf("enter did not commit the filter: %q %d rows", m.filter, len(m.rows))
	}

	// a filter matching nothing empties the table; esc clears it back
	m.Update(keyPress("/"))
	m.Update(keyPress("z"))
	m.Update(keyPress("z"))
	m.Update(keyPress("z"))
	if len(m.rows) != 0 {
		t.Fatalf("unmatched filter left %d rows", len(m.rows))
	}
	m.Update(escKey())
	if m.editing || m.filter != "" || len(m.rows) != 2 {
		t.Fatalf("esc did not clear the filter: editing=%v filter=%q rows=%d",
			m.editing, m.filter, len(m.rows))
	}
}

// TestSortCycleAndReverse covers the s sort-cycle and S reverse keys.
func TestSortCycleAndReverse(t *testing.T) {
	m := newTestModel(t, nil)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{Time: time.Now(),
		Procs: []collector.Proc{
			{PID: 1, Name: "a", CPU: 3, Mem: 1, User: "root"},
			{PID: 2, Name: "b", CPU: 9, Mem: 2, User: "you"},
			{PID: 3, Name: "c", CPU: 6, Mem: 3, User: "me"},
		}}})

	wantOrder := []string{"mem", "pid", "name", "user", "cpu"}
	for _, want := range wantOrder {
		m.Update(keyPress("s"))
		if m.sort.String() != want {
			t.Fatalf("after s: sort = %s, want %s", m.sort, want)
		}
	}
	if m.desc != true {
		t.Fatal("a full sort cycle must not change the direction")
	}

	m.Update(keyPress("S"))
	if m.desc {
		t.Fatal("S did not reverse the sort")
	}
	if v := m.infoView(); !strings.Contains(v, "↑") {
		t.Errorf("info line shows no ascending arrow:\n%s", v)
	}
	m.Update(keyPress("S"))
	if !m.desc {
		t.Fatal("S did not restore the descending sort")
	}

	m.Update(keyPress("s")) // mem again
	if v := m.infoView(); !strings.Contains(v, "mem") || !strings.Contains(v, "↓") {
		t.Errorf("info line should show mem↓:\n%s", v)
	}
}

// TestConfirmFlow drives the signal picker: cycling, the KILL shortcuts,
// cancel, and the send itself. The send always targets ghostPID, which no
// process can own, so the syscall fails harmlessly inside gopsutil's
// existence check and the UI shows the error.
func TestConfirmFlow(t *testing.T) {
	m := newTestModel(t, nil)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{Time: time.Now(),
		Procs: []collector.Proc{{PID: ghostPID, Name: "ghost", State: "S"}}}})

	m.Update(keyPress("k"))
	if m.confirm == nil || m.confirm.pid != ghostPID || m.confirm.sig != 0 {
		t.Fatalf("kill key opened %+v, want the prompt on ghost pid at TERM", m.confirm)
	}

	// left wraps to the last signal
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.confirm.sig != len(m.confirm.sigs)-1 {
		t.Fatalf("left wrapped to %d, want %d", m.confirm.sig, len(m.confirm.sigs)-1)
	}
	// right cycles forward, wrapping at the end
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.confirm.sig != 0 {
		t.Fatalf("right wrapped to %d, want 0", m.confirm.sig)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.confirm.sig != 1 {
		t.Fatalf("right advanced to %d, want 1 (KILL)", m.confirm.sig)
	}
	m.Update(escKey())

	// f jumps straight to KILL from inside the prompt
	m.Update(keyPress("k"))
	m.Update(keyPress("f"))
	if m.confirm.sig != killSignalIndex(m.confirm.sigs) || m.confirm.sigs[m.confirm.sig].Name != "KILL" {
		t.Fatalf("f selected signal %d, want KILL", m.confirm.sig)
	}
	m.Update(escKey())

	// the uppercase force key, pressed outside the prompt, reopens it
	// with KILL preselected
	m.Update(keyPress("K"))
	if m.confirm == nil || m.confirm.sigs[m.confirm.sig].Name != "KILL" {
		t.Fatalf("force key opened %+v, want the prompt at KILL", m.confirm)
	}

	// n cancels without sending
	m.Update(keyPress("n"))
	if m.confirm != nil {
		t.Fatal("n did not cancel the prompt")
	}

	// y sends: against a non-existent pid it must fail into the error
	// status, never signal anything real
	m.Update(keyPress("k"))
	m.Update(keyPress("y"))
	if m.confirm != nil {
		t.Fatal("y did not close the prompt")
	}
	if m.status == "" {
		t.Fatal("y left no feedback status after a failed signal")
	}
}

// TestOpenConfirmGuards covers the states in which the prompt must not
// open.
func TestOpenConfirmGuards(t *testing.T) {
	ro := config.Default()
	ro.ReadOnly = true
	m := newTestModel(t, ro)
	m.Update(collector.SnapshotMsg{Snap: testSnapshot()})
	m.Update(keyPress("k"))
	if m.confirm != nil {
		t.Fatal("read-only view opened the signal prompt")
	}
	if !strings.Contains(m.status, "read-only") {
		t.Fatalf("status = %q, want the read-only notice", m.status)
	}

	// empty table: nothing to select
	empty := newTestModel(t, config.Default())
	if cmd := empty.openConfirm(false); cmd != nil || empty.confirm != nil {
		t.Fatal("openConfirm on an empty table opened the prompt")
	}

	// a stale table (rows restored while the cursor stayed at the
	// out-of-range marker from the empty state) is rejected too
	stale := newTestModel(t, config.Default())
	stale.Update(collector.SnapshotMsg{Snap: testSnapshot()})
	rows := stale.table.Rows()
	stale.table.SetRows(nil)  // cursor drops to the out-of-range marker
	stale.table.SetRows(rows) // rows return, the marker stays
	if cmd := stale.openConfirm(false); cmd != nil || stale.confirm != nil {
		t.Fatal("openConfirm accepted an out-of-range cursor")
	}

	// a valid selection with the force preselect
	full := newTestModel(t, config.Default())
	full.Update(collector.SnapshotMsg{Snap: testSnapshot()})
	full.openConfirm(true)
	if full.confirm == nil || full.confirm.sigs[full.confirm.sig].Name != "KILL" {
		t.Fatalf("force openConfirm picked %+v, want KILL preselected", full.confirm)
	}
	full.confirm = nil
	full.openConfirm(false)
	if full.confirm == nil || full.confirm.sig != 0 {
		t.Fatalf("plain openConfirm picked %+v, want TERM preselected", full.confirm)
	}
}

// TestKillSignalIndex pins the KILL lookup, including the absent case.
func TestKillSignalIndex(t *testing.T) {
	if got := killSignalIndex(nil); got != -1 {
		t.Fatalf("killSignalIndex(nil) = %d, want -1", got)
	}
	if got := killSignalIndex(procs.Signals); got != procs.SignalIndex("KILL") {
		t.Fatalf("killSignalIndex(Signals) = %d, want the KILL index", got)
	}
}

// TestRebuildClearsExitedDetail pins the "selected process exited"
// cleanup: an open detail box for a pid that vanished from the snapshot
// is closed.
func TestRebuildClearsExitedDetail(t *testing.T) {
	m := newTestModel(t, nil)
	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{Time: time.Now(),
		Procs: []collector.Proc{{PID: 7, Name: "doomed"}}}})
	m.detail = &procDetail{proc: collector.Proc{PID: 7, Name: "doomed"}}

	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{Time: time.Now(),
		Procs: []collector.Proc{{PID: 9, Name: "replacement"}}}})
	if m.detail != nil {
		t.Fatal("detail box stayed open for an exited process")
	}
	if len(m.rows) != 1 || m.rows[0].Proc.PID != 9 {
		t.Fatalf("rows = %+v, want only the replacement", m.rows)
	}
}

// TestRowStates renders every process-state color branch at both activity
// column widths.
func TestRowStates(t *testing.T) {
	states := []string{"R", "S", "Z", "T", "I"}
	procs := make([]collector.Proc, len(states))
	for i, s := range states {
		procs[i] = collector.Proc{PID: int32(i + 1), Name: "p" + s, State: s}
	}
	snap := collector.Snapshot{Time: time.Now(), Procs: procs}

	for _, width := range []int{100, 120} { // 100: no ACTIVITY col; 120: with
		m := newTestModel(t, nil)
		m.SetSize(width, 30)
		m.Update(collector.SnapshotMsg{Snap: snap})

		wantCols := 8
		if width >= activityMinWidth {
			wantCols = 9
		}
		if got := len(m.table.Columns()); got != wantCols {
			t.Errorf("width %d: %d columns, want %d", width, got, wantCols)
		}
		for i, row := range m.table.Rows() {
			if len(row) != wantCols {
				t.Errorf("width %d: row %d has %d cells, want %d", width, i, len(row), wantCols)
			}
		}
		if v := m.View(); !strings.Contains(v, "5 procs") {
			t.Errorf("width %d: info line missing the proc count:\n%s", width, v)
		}
	}
}

// TestViewPanels renders every overlay: detail box, signal prompt, filter
// input, plus the info-line decorations.
func TestViewPanels(t *testing.T) {
	fresh := New(config.Default(), theme.ByName(config.Default().Theme))
	if v := fresh.View(); v != "" {
		t.Fatalf("zero-sized view = %q, want empty", v)
	}

	m := newTestModel(t, nil)

	m.Update(collector.SnapshotMsg{Snap: collector.Snapshot{Time: time.Now(),
		Procs: []collector.Proc{
			{PID: ghostPID, Name: "ghost", State: "S", User: "root", Threads: 4, CPU: 12.5, Mem: 2.5, RSS: 1 << 20},
			{PID: 5, Name: "zomb", State: "Z"},
			{PID: 6, Name: "halt", State: "T"},
		}}})

	// info line: session-health flags and the sort direction
	v := m.View()
	for _, want := range []string{"3 procs", "cpu", "↓", "1 zombie", "1 stopped"} {
		if !strings.Contains(v, want) {
			t.Errorf("info line missing %q", want)
		}
	}

	// tree + filter decorations
	m.Update(keyPress("t"))
	m.Update(keyPress("/"))
	m.Update(keyPress("g"))
	m.Update(keyPress("h"))
	if v := m.View(); !strings.Contains(v, "tree") || !strings.Contains(v, "filter:") || !strings.Contains(v, "gh") {
		t.Errorf("info line missing tree/filter decorations:\n%s", v)
	}
	if v := m.View(); !strings.Contains(v, "filter>") {
		t.Error("editing footer should show the input")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(keyPress("S"))
	if v := m.View(); !strings.Contains(v, "↑") {
		t.Errorf("info line missing the ascending arrow:\n%s", v)
	}
	m.status = "SIGTERM sent"
	if v := m.View(); !strings.Contains(v, "SIGTERM sent") {
		t.Errorf("info line should show the action status:\n%s", v)
	}

	// detail box (read-only model: no cmdline lookup on enter)
	ro := config.Default()
	ro.ReadOnly = true
	m2 := newTestModel(t, ro)
	m2.Update(collector.SnapshotMsg{Snap: collector.Snapshot{Time: time.Now(),
		Procs: []collector.Proc{{PID: 4242, Name: "worker", State: "R", User: "ci",
			Threads: 8, Nice: 5, CPU: 42, Mem: 12.5, RSS: 1 << 26}}}})
	m2.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	v = m2.View()
	for _, want := range []string{"PROCESS", "PID", "4242", "ppid 0", "USER", "ci", "STATE", "THREADS",
		"nice 5", "CPU/MEM", "RSS", "worker"} {
		if !strings.Contains(v, want) {
			t.Errorf("detail view missing %q", want)
		}
	}
	// a fetched cmdline replaces the name and wraps at the box width
	m2.detail.cmdline = strings.Repeat("x", 300)
	v = m2.View()
	if !strings.Contains(v, "PROCESS") || !strings.Contains(v, strings.Repeat("x", 50)) {
		t.Error("detail view should render the wrapped command line")
	}

	// signal prompt with its own footer
	m3 := newTestModel(t, nil)
	m3.Update(collector.SnapshotMsg{Snap: collector.Snapshot{Time: time.Now(),
		Procs: []collector.Proc{{PID: ghostPID, Name: "ghost", State: "S"}}}})
	m3.Update(keyPress("k"))
	v = m3.View()
	for _, want := range []string{"SIGNAL", "SIGTERM", "terminate", "2147483000", "ghost",
		"←→", "signal", "y", "send", "K", "kill", "esc", "cancel"} {
		if !strings.Contains(v, want) {
			t.Errorf("confirm view missing %q", want)
		}
	}
}

// procs10 builds ten processes for cursor-movement tests.
func procs10() []collector.Proc {
	out := make([]collector.Proc, 10)
	for i := range out {
		out[i] = collector.Proc{PID: int32(i + 1), Name: "p" + string(rune('a'+i))}
	}
	return out
}
