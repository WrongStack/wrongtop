package docker

// After ui.SetTableColumns clears the rows on a column-count change,
// bubbles' SetRows only clamps the cursor downward: the empty first
// spell parks the cursor at -1 and no rebuild raises it. EnsureCursor
// (called at the end of rebuild) must restore the selection to the
// first row, so keyboard actions work on first render — enter on a
// visible RUNNING container opens the pane instead of setting the
// false status "logs: container is not running".

import (
	"net/http"
	"testing"

	"charm.land/bubbletea/v2"

	"github.com/wrongstack/wrongtop/internal/dockerclient"
	"github.com/wrongstack/wrongtop/internal/ui"
)

func TestCursorRestsOnFirstRow(t *testing.T) {
	cli := startFakeDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.docker.multiplexed-stream")
		_, _ = w.Write(frame("hello\n"))
	})

	m := newTestModel()
	m.SetSize(120, 30) // column count changes: the rows clear parks the cursor
	m.Update(dockerclient.UpdateMsg{Client: cli, Containers: []dockerclient.Container{
		{ID: "abc123", Name: "web", State: "running"},
	}})

	// enter on the visible running container must open the log pane —
	// no arrow-key warm-up, no mouse click
	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.logFor != "web" || cmd == nil {
		t.Fatalf("enter on the visible running container did not open the log pane — cursor=%d, status=%q",
			m.table.Cursor(), ui.StripANSI(m.status))
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // teardown: close the pane
}
