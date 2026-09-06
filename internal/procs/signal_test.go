package procs

import "testing"

func TestSignalIndex(t *testing.T) {
	for i, s := range Signals {
		if got := SignalIndex(s.Name); got != i {
			t.Errorf("SignalIndex(%q) = %d, want %d", s.Name, got, i)
		}
	}
	if got := SignalIndex("NOPE"); got != -1 {
		t.Errorf("SignalIndex(unknown) = %d, want -1", got)
	}
}

func TestAvailableSignals(t *testing.T) {
	got := AvailableSignals()
	if len(got) < 2 {
		t.Fatalf("platform offers %d signals, want at least TERM and KILL", len(got))
	}
	if got[0].Name != "TERM" || got[1].Name != "KILL" {
		t.Fatalf("first two signals = %s, %s; want TERM, KILL", got[0].Name, got[1].Name)
	}
	for _, s := range got {
		if SignalIndex(s.Name) < 0 {
			t.Errorf("available signal %q missing from the picker list", s.Name)
		}
	}
	if extraSignals() && len(got) != len(Signals) {
		t.Errorf("platform with extra signals offers %d, want all %d", len(got), len(Signals))
	}
	if !extraSignals() && len(got) != 2 {
		t.Errorf("platform without extra signals offers %d, want 2", len(got))
	}
}
