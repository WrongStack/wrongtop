package ui

import (
	"charm.land/bubbles/v2/table"
)

// EnsureCursor guarantees the selection indexes a row whenever the
// table has rows. bubbles' SetRows only clamps the cursor downward, so
// the rows-clear in SetTableColumns (or any other empty-table spell)
// parks it at -1 and a later rebuild never raises it — every
// cursor-indexed action then silently dead-ends until an arrow key or
// click moves it. Call after SetRows in rebuild paths.
func EnsureCursor(t *table.Model) {
	if cur := t.Cursor(); (cur < 0 || cur >= len(t.Rows())) && len(t.Rows()) > 0 {
		t.SetCursor(0)
	}
}
