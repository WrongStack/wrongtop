package ui

import (
	"charm.land/bubbles/v2/table"
)

// SetTableColumns replaces a bubbles table's columns without the resize
// panic: the viewport renderer indexes row cells by column count, so a
// count change while old rows are still in place crashes (index out of
// range in renderRow). Rows are cleared whenever the column count
// changes; the caller must rebuild them right after — every tab does,
// from its latest snapshot.
func SetTableColumns(t *table.Model, cols []table.Column) {
	if len(t.Columns()) == len(cols) { // same shape: keep the rows
		t.SetColumns(cols)
		return
	}
	t.SetRows(nil)
	t.SetColumns(cols)
}
