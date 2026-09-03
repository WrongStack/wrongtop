package canvas

import "testing"

func TestScaleJumpsToSpikes(t *testing.T) {
	var s Scale
	if got := s.Observe(100); got != 100 {
		t.Errorf("spike should raise the ceiling instantly: got %v, want 100", got)
	}
	if got := s.Observe(200); got != 200 {
		t.Errorf("bigger spike should raise the ceiling: got %v, want 200", got)
	}
}

func TestScaleDecays(t *testing.T) {
	var s Scale
	s.Observe(1000)
	s.Observe(10) // first sample after the spike decays 2%
	if got, want := s.Max(), 980.0; got != want {
		t.Errorf("after one decay step: got %v, want %v", got, want)
	}
	// repeated quiet samples decay geometrically toward the quiet value
	for range 100 {
		s.Observe(10)
	}
	if s.Max() < 10 || s.Max() > 200 {
		t.Errorf("ceiling should converge near the quiet value: %v", s.Max())
	}
}

func TestScaleNeverBelowSampleOrOne(t *testing.T) {
	var s Scale
	if got := s.Observe(0.25); got != 0.25 {
		t.Errorf("ceiling should track tiny quiet values: got %v", got)
	}
	s.Observe(500)
	if got := s.Observe(0); got < 1 {
		t.Errorf("ceiling floor is 1: got %v", got)
	}
}
