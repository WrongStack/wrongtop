package app

// The root model routes tab-internal async completion messages — a
// docker log line, a lifecycle action report, a background cmdline
// fetch — to every tab, not just the active one: the tab that armed a
// wait keeps receiving its completions however far it is from the
// active tab (tab switching never cancels a running stream). These
// tests pin that contract for the docker tab's two background flows.

import (
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/dockerclient"
)

// dockerLogFrame builds one multiplexed docker log-stream frame
// (stdout, big-endian length header) as the daemon would send it.
func dockerLogFrame(payload string) []byte {
	b := make([]byte, 8+len(payload))
	b[0] = 0x01
	binary.BigEndian.PutUint32(b[4:8], uint32(len(payload)))
	copy(b[8:], payload)
	return b
}

// runCmdMsg executes cmd with a timeout guard and returns its message.
func runCmdMsg(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatalf("command did not complete within 5s")
		return nil
	}
}

// execToLeaf runs cmd down through BatchMsg layers to a leaf message;
// the driver assumes single-command batches (the app only batches tab
// completion dispatch, one owner per message).
func execToLeaf(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	msg := runCmdMsg(t, cmd)
	for {
		batch, ok := msg.(tea.BatchMsg)
		if !ok {
			return msg
		}
		if len(batch) != 1 {
			t.Fatalf("unexpected batch shape: %d commands", len(batch))
		}
		msg = runCmdMsg(t, batch[0])
	}
}

// TestLogLinesReachHiddenDockerTab pins the log-stream half of the
// contract: with the log pane open, switching to another tab must not
// drop the line the armed wait consumed mid-switch — the hidden docker
// tab appends it and re-arms, so returning shows a pane that kept
// following. (The inverse — a logLineMsg delivered while the pane is
// closed — is pinned in the docker package.)
func TestLogLinesReachHiddenDockerTab(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			w.Header().Set("API-Version", "1.45")
			_, _ = w.Write([]byte("OK"))
		case strings.HasSuffix(r.URL.Path, "/logs"):
			// both lines up front, then return: the contract under test
			// is message delivery routing, not daemon-side stream
			// timing, and a returning handler keeps the connection free
			// for the SDK's follow-up inspect call
			w.Header().Set("Content-Type", "application/vnd.docker.multiplexed-stream")
			_, _ = w.Write(dockerLogFrame("one\n"))
			_, _ = w.Write(dockerLogFrame("two\n"))
		default:
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

	m := New(config.Default(), "", "test")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(dockerclient.UpdateMsg{Client: cli, Containers: []dockerclient.Container{
		{ID: "abc123", Name: "web", State: "running"},
	}})

	dockerIdx := -1
	for i, tab := range m.tabs {
		if strings.Contains(tab.Title(), "DOCKER") {
			dockerIdx = i
		}
	}
	if dockerIdx < 0 {
		t.Fatal("no docker tab in the default module set")
	}
	// teardown before the server closes: kill any open log pane so the
	// pump never outlives the test even when an assertion fails mid-flight
	t.Cleanup(func() {
		m.Update(key(strconv.Itoa(dockerIdx + 1)))
		m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	})

	m.Update(key(strconv.Itoa(dockerIdx + 1))) // activate via the real key path
	if m.active != dockerIdx {
		t.Fatalf("tab switch failed: active=%d want %d", m.active, dockerIdx)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // move the cursor onto the row

	// enter opens the log pane and arms the first wait
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("enter on a running container did not arm the log wait; docker view: %q",
			m.tabs[dockerIdx].View())
	}
	msgOne := execToLeaf(t, cmd)
	_, next := m.Update(msgOne) // docker is active: the chain must continue
	if next == nil {
		t.Fatal("wait chain broke while the docker tab was active")
	}
	if v := m.tabs[dockerIdx].View(); !strings.Contains(v, "one") {
		t.Fatalf("active-tab log line missing from the pane: %q", v)
	}

	// the second line is already in the pump buffer; the armed wait
	// consumes it and produces the message the runtime would deliver
	line2 := execToLeaf(t, next)

	// switch away through the real key path, then deliver line two:
	// the hidden docker tab must append it and re-arm the wait
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	_, after := m.Update(line2)

	m.Update(key(strconv.Itoa(dockerIdx + 1))) // back to the docker tab
	v := m.tabs[dockerIdx].View()
	if after == nil || !strings.Contains(v, "two") {
		t.Fatalf("log line dropped while the docker tab was hidden — pane frozen at %q (re-arm cmd: %v)", v, after)
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // teardown: close the pane
}

// TestActionDoneReachesHiddenDockerTab pins the action-report half: a
// lifecycle action started before a tab switch must still update the
// status line while the tab is hidden. actionDoneMsg is terminal, so
// the observable is the status text, not a re-armed command.
func TestActionDoneReachesHiddenDockerTab(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			w.Header().Set("API-Version", "1.45")
			_, _ = w.Write([]byte("OK"))
		default:
			w.WriteHeader(http.StatusNotFound) // start fails: that is the report
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("DOCKER_HOST", "tcp://"+strings.TrimPrefix(srv.URL, "http://"))
	cli, err := dockerclient.New()
	if err != nil {
		t.Fatalf("connecting to the fake daemon: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	m := New(config.Default(), "", "test")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(dockerclient.UpdateMsg{Client: cli, Containers: []dockerclient.Container{
		{ID: "abc123", Name: "web", State: "running"},
	}})

	dockerIdx := -1
	for i, tab := range m.tabs {
		if strings.Contains(tab.Title(), "DOCKER") {
			dockerIdx = i
		}
	}
	m.Update(key(strconv.Itoa(dockerIdx + 1)))
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // move the cursor onto the row

	_, cmd := m.Update(key("s")) // arm the start action
	if cmd == nil {
		t.Fatal("'s' on a running container did not arm the action")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // hide the tab mid-action
	msg := execToLeaf(t, cmd)                   // action completes: 404 -> error report
	m.Update(msg)
	m.Update(key(strconv.Itoa(dockerIdx + 1))) // back to the docker tab
	v := m.tabs[dockerIdx].View()
	if !strings.Contains(v, "start: ") {
		t.Fatalf("action report dropped while the docker tab was hidden — head line %q", v)
	}
}
