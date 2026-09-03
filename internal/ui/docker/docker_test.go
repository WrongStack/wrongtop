package docker

import (
	"testing"

	"github.com/ersinkoc/wrongtop/internal/config"
	"github.com/ersinkoc/wrongtop/internal/theme"
)

func newTestModel() *Model {
	cfg := config.Default()
	return New(cfg, theme.ByName(cfg.Theme))
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
