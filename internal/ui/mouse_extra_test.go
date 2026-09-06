package ui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/table"
)

// A lone trailing ESC is dropped instead of leaking into the output.
func TestStripANSITrailingEscape(t *testing.T) {
	if got := StripANSI("ok\x1b"); got != "ok" {
		t.Errorf("StripANSI(%q) = %q, want %q", "ok\x1b", got, "ok")
	}
	if got := StripANSI("\x1b"); got != "" {
		t.Errorf("StripANSI of a lone ESC = %q, want \"\"", got)
	}
}

// ClickedRowIndex must skip rows that render as blank lines and locate
// real rows by content, whatever sits between them in the data.
func TestClickedRowIndexAroundBlankRows(t *testing.T) {
	tb := table.New(
		table.WithColumns([]table.Column{{Title: "KEY", Width: 7}, {Title: "EXTRA", Width: 6}}),
		table.WithWidth(30),
		table.WithHeight(8),
	)
	tb.SetRows([]table.Row{
		{"row-0", "x"},
		{}, // renders as a blank line
		{"row-2", "y"},
	})

	// locate the clicked lines by content so the test does not depend on
	// bubbles' exact viewport height
	lines := strings.Split(tb.View(), "\n")
	blank, last := -1, -1
	for i, l := range lines {
		s := strings.TrimSpace(StripANSI(l))
		if s == "" && blank == -1 && i > 0 {
			blank = i
		}
		if strings.Contains(s, "row-2") {
			last = i
		}
	}
	if blank == -1 || last == -1 {
		t.Fatalf("expected a blank row line and the last row, got blank=%d last=%d:\n%q",
			blank, last, tb.View())
	}
	if got := ClickedRowIndex(tb, blank-1); got != -1 {
		t.Errorf("clicking the blank line (viewport row %d) = %d, want -1", blank-1, got)
	}
	if got := ClickedRowIndex(tb, last-1); got != 2 {
		t.Errorf("clicking the last row (viewport row %d) = %d, want 2", last-1, got)
	}
}

// A table whose first column is unusable cannot map clicks to rows.
func TestClickedRowIndexWithoutUsableFirstColumn(t *testing.T) {
	noCols := table.New(table.WithWidth(20), table.WithHeight(4))
	if got := ClickedRowIndex(noCols, 0); got != -1 {
		t.Errorf("no columns: ClickedRowIndex = %d, want -1", got)
	}
	// the second column keeps the clicked line non-blank, so the guard is
	// what returns -1 (not the blank-line check)
	zeroWidth := table.New(
		table.WithColumns([]table.Column{{Title: "A", Width: 0}, {Title: "B", Width: 5}}),
		table.WithWidth(20),
		table.WithHeight(4),
	)
	zeroWidth.SetRows([]table.Row{{"a", "b"}})
	if got := ClickedRowIndex(zeroWidth, 0); got != -1 {
		t.Errorf("zero-width first column: ClickedRowIndex = %d, want -1", got)
	}
}

// Out-of-range indexes must not scroll or otherwise disturb the table.
func TestEnsureRowVisibleIgnoresOutOfRange(t *testing.T) {
	tb := newTestTable(3, 5)
	before := tb.View()
	EnsureRowVisible(&tb, -1)
	EnsureRowVisible(&tb, 99)
	if tb.View() != before {
		t.Error("out-of-range indexes must leave the table untouched")
	}
}

// padCell reproduces the bubbles cell renderer: truncate with an
// ellipsis, then pad with spaces.
func TestPadCell(t *testing.T) {
	cases := []struct {
		name string
		v    string
		w    int
		want string
	}{
		{"zero width", "abc", 0, ""},
		{"negative width", "abc", -2, ""},
		{"truncate marks the cut", "abcdefgh", 5, "abcd…"},
		{"pad short values", "ab", 5, "ab   "},
		{"exact fit", "abc", 3, "abc"},
		{"empty value pads", "", 3, "   "},
		{"cut at rune boundary", "数据库服务", 3, "数据…"},
	}
	for _, tc := range cases {
		if got := padCell(tc.v, tc.w); got != tc.want {
			t.Errorf("%s: padCell(%q, %d) = %q, want %q", tc.name, tc.v, tc.w, got, tc.want)
		}
	}
}
