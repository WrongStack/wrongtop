package collector

import "testing"

func TestAbbrevState(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// gopsutil maps unknown ps state chars to "" (its UnknownState
		// constant) and returns it with a nil error — e.g. darwin's
		// unmapped "X" dead state; it must render as unknown, not panic
		{"", "?"},
		{"   ", "?"},
		// documented mappings
		{"running", "R"}, {"r", "R"},
		{"sleeping", "S"}, {"s", "S"},
		{"zombie", "Z"}, {"z", "Z"},
		{"stopped", "T"}, {"traced", "T"}, {"t", "T"},
		{"idle", "I"}, {"i", "I"},
		{"waiting", "W"}, {"w", "W"},
		{"blocked", "B"}, {"b", "B"},
		{"locked", "L"}, {"l", "L"},
		// unknown states pass their first letter through
		{"D", "D"}, // linux uninterruptible sleep
		{"X", "X"},
		{"?", "?"},
		{"rx", "R"},
		// normalization: case and surrounding space
		{" R ", "R"},
		{"Zombie", "Z"},
		{"RUNNING", "R"},
	}
	for _, c := range cases {
		if got := abbrevState(c.in); got != c.want {
			t.Errorf("abbrevState(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
