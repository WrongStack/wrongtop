package docker

// logLineMsg carries the stream it came from, and Update drops lines
// whose stream is not the active one: the wait chain can outlive its
// stream (pane closed, or closed and reopened on another container),
// and a line delivered after such a replacement must never land in the
// new container's buffer.

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/wrongstack/wrongtop/internal/dockerclient"
)

// runOne executes cmd with a timeout guard and returns its message.
func runOne(t *testing.T, cmd tea.Cmd) tea.Msg {
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

func TestStaleLineFromReplacedStreamDropped(t *testing.T) {
	cli := startFakeDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.docker.multiplexed-stream")
		if strings.Contains(r.URL.Path, "aaaa") {
			_, _ = w.Write(frame("from-web\n"))
			return
		}
		_, _ = w.Write(frame("from-db\n"))
	})

	m := newTestModel()
	m.SetSize(120, 30)
	m.Update(dockerclient.UpdateMsg{Client: cli, Containers: []dockerclient.Container{
		{ID: "aaaa", Name: "web", State: "running"},
		{ID: "bbbb", Name: "db", State: "running"},
	}})

	// follow web: the armed wait consumes web's first line
	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msgA := runOne(t, cmd)

	// close the pane, reopen on db: the in-flight line from web's
	// stream arrives under db's open pane and must be dropped
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	cmdB := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := m.Update(msgA); got != nil {
		t.Fatalf("stale line from web's stream accepted into db's pane — logs=%v (re-arm: %v)", m.logs, got)
	}
	if len(m.logs) != 0 {
		t.Fatalf("stale line polluted db's buffer: %v", m.logs)
	}

	// db's own stream still delivers through the legit wait
	line := runOne(t, cmdB)
	if got := m.Update(line); got == nil {
		t.Fatal("db's own line was dropped")
	}
	if len(m.logs) != 1 || !strings.Contains(m.logs[0], "from-db") {
		t.Fatalf("db's pane holds %v, want [from-db]", m.logs)
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // teardown: close the pane
}
