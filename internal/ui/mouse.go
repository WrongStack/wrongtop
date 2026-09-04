package ui

import (
	"strings"

	"charm.land/bubbles/v2/table"
)

// StripANSI removes ANSI escape sequences (CSI and OSC) from s so that
// rendered rows can be matched against plain text.
func StripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			break
		}
		switch s[i+1] {
		case '[': // CSI: swallow up to the final byte (0x40–0x7e)
			for i += 2; i < len(s) && (s[i] < 0x40 || s[i] > 0x7e); i++ {
			}
		case ']': // OSC: swallow up to BEL or ST
			for i += 2; i < len(s); i++ {
				if s[i] == 0x07 {
					break
				}
				if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
					i++
					break
				}
			}
		default: // two-byte escape (e.g. ESC M)
			i++
		}
	}
	return b.String()
}

// ClickedRowIndex maps a mouse click on a bubbles table to the index of
// the data row displayed viewportRow lines below the header. The table
// keeps its scroll offset private, so rows are located by content
// instead: cells render truncated and space-padded to the column width,
// which lets us reproduce each row's first cell and match it against the
// ANSI-stripped clicked line — correct no matter how far the table has
// scrolled. Returns -1 when the click misses every row.
func ClickedRowIndex(t table.Model, viewportRow int) int {
	lines := strings.Split(t.View(), "\n")
	i := viewportRow + 1 // lines[0] is the header row
	if i < 0 || i >= len(lines) {
		return -1
	}
	line := StripANSI(lines[i])
	if strings.TrimSpace(line) == "" {
		return -1
	}
	cols := t.Columns()
	rows := t.Rows()
	if len(cols) == 0 || cols[0].Width <= 0 {
		return -1
	}
	w := cols[0].Width
	for idx, row := range rows {
		if len(row) == 0 {
			continue
		}
		cell := padCell(row[0], w)
		if cell == "" {
			continue
		}
		// the default cell style pads each cell with one blank column on
		// either side; accept both padded and bare forms so a future
		// style change degrades instead of breaking the mapping
		if strings.HasPrefix(line, " "+cell+" ") || strings.HasPrefix(line, cell) {
			return idx
		}
	}
	return -1
}

// EnsureRowVisible scrolls the table so the row at data index idx is on
// screen, without moving the selection. Bubbles' SetCursor re-renders the
// viewport content around the cursor but never adjusts its scroll offset,
// so a restored selection can silently end up just off-screen. The check
// scans the displayed lines; the fix jumps to the row btop-style, pinned
// to the bottom edge.
func EnsureRowVisible(t *table.Model, idx int) {
	if idx < 0 || idx >= len(t.Rows()) {
		return
	}
	for r := 0; r < t.Height(); r++ {
		if ClickedRowIndex(*t, r) == idx {
			return // already displayed
		}
	}
	t.GotoTop()
	t.MoveDown(idx)
}

// padCell reproduces how bubbles renders a plain table cell: truncate to
// w runes (marking the cut with an ellipsis), then pad with spaces to w.
func padCell(v string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(v)
	if len(r) > w {
		r = r[:w-1]
		v = string(r) + "…"
	}
	var b strings.Builder
	b.WriteString(v)
	for len([]rune(b.String())) < w {
		b.WriteByte(' ')
	}
	return b.String()
}
