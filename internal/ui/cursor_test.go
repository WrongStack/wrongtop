package ui

import (
	"charm.land/bubbles/v2/table"
	"testing"
)

func TestEnsureCursorRestoresAfterRowsClear(t *testing.T) {
	tb := table.New()
	tb.SetColumns([]table.Column{{Title: "A", Width: 5}})
	tb.SetRows([]table.Row{{"x"}})

	// the resize path: a column-count change clears the rows, parking
	// the cursor at -1 (SetRows clamps downward only); the rebuild that
	// follows never raises it
	SetTableColumns(&tb, []table.Column{{Title: "A", Width: 5}, {Title: "B", Width: 5}})
	tb.SetRows([]table.Row{{"x"}})
	if tb.Cursor() != -1 {
		t.Fatalf("precondition: cleared-rebuild cursor = %d, want -1", tb.Cursor())
	}

	EnsureCursor(&tb)
	if tb.Cursor() != 0 {
		t.Fatalf("cursor = %d, want 0 after EnsureCursor", tb.Cursor())
	}

	// an already-valid cursor is left alone
	EnsureCursor(&tb)
	if tb.Cursor() != 0 {
		t.Fatalf("cursor = %d, want unchanged 0", tb.Cursor())
	}

	// an empty table has nothing to select: no panic, cursor untouched
	empty := table.New()
	empty.SetRows(nil)
	if empty.Cursor() != -1 {
		t.Fatalf("precondition: cleared empty-table cursor = %d, want -1", empty.Cursor())
	}
	EnsureCursor(&empty)
	if empty.Cursor() != -1 {
		t.Fatalf("empty-table cursor = %d, want untouched -1", empty.Cursor())
	}
}
