package dashboard

import (
	"testing"
	"unicode/utf8"
)

// TestTruncKeepsValidUTF8 guards the trunc contract: output is always
// valid UTF-8, cut at rune boundaries (never inside a multi-byte rune),
// with ASCII behavior unchanged. trunc counts runes, not display cells —
// wide runes may render wider than w columns, which is pre-existing
// panel semantics and intentionally not covered here.
func TestTruncKeepsValidUTF8(t *testing.T) {
	const name = "José García" // 13 bytes, 11 runes; a byte-cut at 4 is mid-rune
	if got := trunc(name, 5); !utf8.ValidString(got) || got != "José…" {
		t.Fatalf("trunc(%q, 5) = %q, want valid UTF-8 %q", name, got, "José…")
	}
	// cut between wide runes
	if got := trunc("数据库服务", 4); got != "数据库…" {
		t.Fatalf("trunc CJK = %q, want %q", got, "数据库…")
	}
	// n == 1: everything is dropped, only the ellipsis remains
	if got := trunc("任意", 1); got != "…" {
		t.Fatalf("trunc w=1 = %q, want %q", got, "…")
	}
	// ASCII behavior unchanged
	if got := trunc("dockerd", 5); got != "dock…" {
		t.Fatalf("trunc ASCII = %q, want %q", got, "dock…")
	}
	// not longer than w: returned unchanged
	if got := trunc("abc", 5); got != "abc" {
		t.Fatalf("trunc short = %q, want %q", got, "abc")
	}
	// long in bytes but short in runes: returned unchanged
	const emoji = "🚀🚀🚀" // 12 bytes, 3 runes
	if got := trunc(emoji, 5); got != emoji {
		t.Fatalf("trunc bytes>w runes<=w = %q, want unchanged %q", got, emoji)
	}
	// this copy's distinguishing guard: w < 1 yields the empty string
	if got := trunc("anything", 0); got != "" {
		t.Fatalf("trunc w=0 = %q, want \"\"", got)
	}
}
