package ui

// Trunc shortens s to at most w runes plus an ellipsis, cutting at rune
// boundaries so multi-byte names stay valid UTF-8.
func Trunc(s string, w int) string {
	if len(s) <= w {
		return s
	}
	if w < 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w { // long in bytes, short in runes: nothing to drop
		return s
	}
	return string(r[:w-1]) + "…"
}
