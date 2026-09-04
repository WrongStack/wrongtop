package ui

import (
	"fmt"
	"testing"

	"charm.land/bubbles/v2/table"
)

func newTestTable(n, height int) table.Model {
	t := table.New(
		table.WithColumns([]table.Column{{Title: "PID", Width: 7}, {Title: "NAME", Width: 10}}),
		table.WithWidth(40),
		table.WithHeight(height),
		table.WithFocused(true),
	)
	rows := make([]table.Row, n)
	for i := range rows {
		rows[i] = table.Row{fmt.Sprintf("%7d", 1000+i), fmt.Sprintf("proc-%02d", i)}
	}
	t.SetRows(rows)
	return t
}

// ClickedRowIndex must map every displayed line back to the data row
// rendered on it, whatever the internal scroll offset is.
func TestClickedRowIndexUnscrolledAndScrolled(t *testing.T) {
	tb := newTestTable(30, 5)
	height := tb.Height() // SetHeight subtracts the header row

	// unscrolled: viewport rows map 1:1 onto data rows
	for r := 0; r < height; r++ {
		if got := ClickedRowIndex(tb, r); got != r {
			t.Fatalf("unscrolled: ClickedRowIndex(%d) = %d, want %d", r, got, r)
		}
	}

	// scrolled: rows stay contiguous, cover the cursor and stay in bounds.
	// SetCursor alone leaves the viewport offset stale in bubbles, so the
	// app pairs it with EnsureRowVisible — mirror that here.
	for _, cursor := range []int{1, 10, 17, 29} {
		tb.SetCursor(cursor)
		EnsureRowVisible(&tb, cursor)
		prev := -1
		found := false
		for r := 0; r < height; r++ {
			got := ClickedRowIndex(tb, r)
			if got == -1 {
				continue
			}
			if found && got != prev+1 {
				t.Fatalf("cursor %d: viewport rows not contiguous: %d then %d", cursor, prev, got)
			}
			found = true
			prev = got
		}
		if min, max := prev-height+1, prev; cursor < min || cursor > max {
			t.Fatalf("cursor %d: displayed window [%d,%d] does not contain it", cursor, max-height+1, max)
		}
	}
}

// EnsureRowVisible must leave a freshly jumped-to row on screen even when
// SetCursor parked it outside the displayed window.
func TestEnsureRowVisible(t *testing.T) {
	tb := newTestTable(30, 5)
	height := tb.Height()
	tb.SetCursor(10) // stale offset: bubbles displays rows below the cursor
	EnsureRowVisible(&tb, 10)
	visible := map[int]bool{}
	for r := 0; r < height; r++ {
		if got := ClickedRowIndex(tb, r); got >= 0 {
			visible[got] = true
		}
	}
	if !visible[10] {
		t.Fatalf("row 10 still not displayed after EnsureRowVisible: %v", visible)
	}
}

func TestClickedRowIndexMisses(t *testing.T) {
	tb := newTestTable(3, 5)
	for _, r := range []int{-1, 99} {
		if got := ClickedRowIndex(tb, r); got != -1 {
			t.Errorf("ClickedRowIndex(%d) = %d, want -1", r, got)
		}
	}
}

// First-cell matching keeps working for multibyte names: the PID column
// stays plain ASCII, so those rows must still resolve.
func TestClickedRowIndexMultibyte(t *testing.T) {
	tb := table.New(
		table.WithColumns([]table.Column{{Title: "PID", Width: 7}, {Title: "NAME", Width: 12}}),
		table.WithWidth(40),
		table.WithHeight(6),
		table.WithFocused(true),
	)
	tb.SetRows([]table.Row{
		{"   1001", "proc-a"},
		{"   1002", "kağan-öztürk"},
		{"   1003", "proc-c"},
	})
	if got := ClickedRowIndex(tb, 1); got != 1 {
		t.Fatalf("multibyte row: ClickedRowIndex(1) = %d, want 1", got)
	}
}

func TestStripANSI(t *testing.T) {
	cases := map[string]string{
		"\x1b[31mred\x1b[0m":          "red",
		"a\x1b[1;32mb\x1b[?25lc":      "abc",
		"\x1b]0;title\x07after":       "after",
		"\x1b]8;;url\x1b\\link\x1b\\": "link",
		"plain":                       "plain",
	}
	for in, want := range cases {
		if got := StripANSI(in); got != want {
			t.Errorf("StripANSI(%q) = %q, want %q", in, got, want)
		}
	}
}
