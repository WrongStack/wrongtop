package ui

import (
	"testing"

	"charm.land/bubbles/v2/table"
)

// Reshaping columns clears the rows (bubbles would panic indexing old
// rows against the new column count); keeping the shape keeps the rows.
func TestSetTableColumns(t *testing.T) {
	tb := newTestTable(2, 5)

	SetTableColumns(&tb, []table.Column{{Title: "A", Width: 7}, {Title: "B", Width: 9}})
	if got := len(tb.Rows()); got != 2 {
		t.Errorf("same column count should keep the rows, have %d", got)
	}
	if title := tb.Columns()[0].Title; title != "A" {
		t.Errorf("columns should be replaced in place, first title = %q", title)
	}

	SetTableColumns(&tb, []table.Column{
		{Title: "A", Width: 7}, {Title: "B", Width: 9}, {Title: "C", Width: 5},
	})
	if got := len(tb.Columns()); got != 3 {
		t.Errorf("column count = %d, want 3", got)
	}
	if got := len(tb.Rows()); got != 0 {
		t.Errorf("a column count change must clear the rows, have %d", got)
	}

	SetTableColumns(&tb, []table.Column{{Title: "ONLY", Width: 9}})
	if got := len(tb.Columns()); got != 1 {
		t.Errorf("shrinking to %d columns, want 1", got)
	}
}
