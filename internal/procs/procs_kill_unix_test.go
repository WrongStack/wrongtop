//go:build !windows

package procs

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

// startSleeper spawns a throwaway `sleep` child so the signaling tests
// only ever touch a process this test owns. Cleanup kills and reaps it
// even when the test signals something else.
func startSleeper(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start a sleep child: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

// freePID returns a pid no live process currently owns, so error paths
// never deliver a signal to an unknown victim.
func freePID(t *testing.T) int32 {
	t.Helper()
	for pid := int32(2147483000); pid > 2147482000; pid-- {
		if err := syscall.Kill(int(pid), 0); err == syscall.ESRCH {
			return pid
		}
	}
	t.Skip("no free high pid found")
	return 0
}

func TestActionsRefuseSelf(t *testing.T) {
	self := int32(os.Getpid())
	if err := Kill(self); err == nil {
		t.Error("Kill must refuse the wrongtop pid")
	}
	if err := ForceKill(self); err == nil {
		t.Error("ForceKill must refuse the wrongtop pid")
	}
	if err := SendSignal(self, "TERM"); err == nil {
		t.Error("SendSignal must refuse the wrongtop pid")
	}
}

func TestActionsOnMissingProcess(t *testing.T) {
	pid := freePID(t)
	if err := Kill(pid); err == nil {
		t.Error("Kill on a missing pid should fail")
	}
	if err := ForceKill(pid); err == nil {
		t.Error("ForceKill on a missing pid should fail")
	}
	if err := SendSignal(pid, "TERM"); err == nil {
		t.Error("SendSignal TERM on a missing pid should fail")
	}
	if err := SendSignal(pid, "NOPE"); err == nil {
		t.Error("SendSignal with an unknown name on a missing pid should fail")
	}
}

func TestKillTerminatesChild(t *testing.T) {
	cmd := startSleeper(t)
	if err := Kill(int32(cmd.Process.Pid)); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Error("child should have died from SIGTERM")
	}
}

func TestForceKillChild(t *testing.T) {
	cmd := startSleeper(t)
	if err := ForceKill(int32(cmd.Process.Pid)); err != nil {
		t.Fatalf("ForceKill: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Error("child should have died from SIGKILL")
	}
}

func TestSendSignalTermAndKill(t *testing.T) {
	cmd := startSleeper(t)
	if err := SendSignal(int32(cmd.Process.Pid), "TERM"); err != nil {
		t.Fatalf("SendSignal TERM: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Error("child should have died from SIGTERM")
	}

	cmd = startSleeper(t)
	if err := SendSignal(int32(cmd.Process.Pid), "KILL"); err != nil {
		t.Fatalf("SendSignal KILL: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Error("child should have died from SIGKILL")
	}
}

func TestSendSignalTerminatingExtras(t *testing.T) {
	for _, name := range []string{"INT", "HUP", "QUIT"} {
		cmd := startSleeper(t)
		if err := SendSignal(int32(cmd.Process.Pid), name); err != nil {
			t.Fatalf("SendSignal %s: %v", name, err)
		}
		if err := cmd.Wait(); err == nil {
			t.Errorf("child should have died from SIG%s", name)
		}
	}
}

func TestSendSignalStopAndContinue(t *testing.T) {
	cmd := startSleeper(t)
	pid := int32(cmd.Process.Pid)
	if err := SendSignal(pid, "STOP"); err != nil {
		t.Fatalf("SendSignal STOP: %v", err)
	}
	if err := SendSignal(pid, "CONT"); err != nil {
		t.Fatalf("SendSignal CONT: %v", err)
	}
	// the child must still be signal-able after a stop/resume round trip;
	// cleanup reaps it
	if err := SendSignal(pid, "INT"); err != nil {
		t.Fatalf("SendSignal INT after STOP/CONT: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Error("child should have died from SIGINT")
	}
}

func TestSendSignalUnknownName(t *testing.T) {
	cmd := startSleeper(t)
	err := SendSignal(int32(cmd.Process.Pid), "NOPE")
	if err == nil {
		t.Fatal("unknown signal name should be rejected")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("error = %v, want the not-available message", err)
	}
	// the child survives the rejected request; cleanup reaps it
}
