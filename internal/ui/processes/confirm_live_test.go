package processes

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
)

// TestConfirmSendsRealSignal delivers SIGTERM to a process the test
// spawned and owns — the one safe way to exercise the success path of
// the kill confirmation. The child is reaped by Wait regardless of how
// it terminates.
func TestConfirmSendsRealSignal(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn a child process: %v", err)
	}
	reaped := false // Wait's result is consumed exactly once
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		if !reaped {
			_ = cmd.Process.Kill()
			<-done
		}
	})

	cfg := config.Default()
	m := newTestModel(t, cfg)
	snap := collector.Snapshot{Time: time.Now(), Procs: []collector.Proc{{
		PID: int32(cmd.Process.Pid), Name: "sleep", CPU: 0, Mem: 0, RSS: 1 << 20,
		User: "self", State: "S", Threads: 1,
	}}}
	m.Update(collector.SnapshotMsg{Snap: snap})

	m.Update(keyPress(cfg.Keys.Kill)) // open the signal menu (TERM preselected)
	if m.confirm == nil {
		t.Fatal("kill key did not open the confirm dialog")
	}
	m.Update(keyPress("y"))
	if m.confirm != nil {
		t.Fatal("y did not close the confirm dialog")
	}
	if !strings.Contains(stripStatus(m.status), "SIGTERM sent") {
		t.Fatalf("success status missing: %q", stripStatus(m.status))
	}

	select { // the signal must actually reach the child
	case <-done:
		reaped = true
	case <-time.After(5 * time.Second):
		t.Fatal("child did not exit after SIGTERM")
	}
}

// stripStatus removes SGR sequences so the status line is readable.
func stripStatus(s string) string {
	var b []rune
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
		b = append(b, r)
	}
	return string(b)
}
