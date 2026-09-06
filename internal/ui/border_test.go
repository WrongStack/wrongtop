package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestBorderFor(t *testing.T) {
	cases := []struct {
		name    string
		border  string
		topLeft string
	}{
		{"square", "square", "┌"},
		{"thick", "thick", "┏"},
		{"double", "double", "╔"},
		{"rounded default", "rounded", "╭"},
		{"unknown falls back to rounded", "wavy", "╭"},
		{"empty falls back to rounded", "", "╭"},
	}
	for _, tc := range cases {
		if got := BorderFor(tc.border).TopLeft; got != tc.topLeft {
			t.Errorf("%s: BorderFor(%q).TopLeft = %q, want %q", tc.name, tc.border, got, tc.topLeft)
		}
	}
}

func TestBoxChrome(t *testing.T) {
	border := lipgloss.RoundedBorder()
	boxStyle := lipgloss.NewStyle().Border(border)
	charStyle := lipgloss.NewStyle()
	titleStyle := lipgloss.NewStyle().Bold(true)

	out := Box(border, boxStyle, charStyle, titleStyle, "T", "R", "hello")
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("boxed lines = %d, want 3 (bordered body)", len(lines))
	}
	top := stripANSIFrame(lines[0])
	// inner content width 5, title 3, readout 1 -> one border fill rune
	if want := "╭ T ─R╮"; top != want {
		t.Errorf("top chrome = %q, want %q", top, want)
	}
	if got := lipgloss.Width(lines[0]); got != 7 {
		t.Errorf("top line width = %d, want 7", got)
	}
	if body := stripANSIFrame(lines[1]); body != "│hello│" {
		t.Errorf("body = %q, want %q", body, "│hello│")
	}
}

// Box pads the content so title + readout always fit on the top border.
func TestBoxChromeFitsWithoutFill(t *testing.T) {
	border := lipgloss.NormalBorder()
	out := Box(border, lipgloss.NewStyle(), lipgloss.NewStyle(),
		lipgloss.NewStyle(), "T", "RRRR", "")
	lines := strings.Split(out, "\n")
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	if got, want := stripANSIFrame(lines[0]), "┌ T RRRR┐"; got != want {
		t.Errorf("top chrome = %q, want %q (fill exactly zero)", got, want)
	}
	if got := lipgloss.Width(lines[0]); got != 9 {
		t.Errorf("top line width = %d, want 9", got)
	}
}

// A missing readout leaves no gap: the border runs to the corner.
func TestBoxNoReadout(t *testing.T) {
	border := lipgloss.RoundedBorder()
	out := Box(border, lipgloss.NewStyle(), lipgloss.NewStyle(),
		lipgloss.NewStyle(), "T", "", "hi")
	lines := strings.Split(out, "\n")
	if got, want := stripANSIFrame(lines[0]), "╭ T ╮"; got != want {
		t.Errorf("top chrome = %q, want %q", got, want)
	}
	if got := lipgloss.Width(lines[0]); got != 5 {
		t.Errorf("top line width = %d, want 5 (inner pads to the title)", got)
	}
}

func TestIcon(t *testing.T) {
	cases := []struct {
		name string
		nerd bool
		want string
	}{
		{"host", false, "⌂"},
		{"fan", false, "❉"},
		{"host", true, "\uf015"},
		{"temp", true, "\uf2c9"}, // nerd-only glyph
		{"temp", false, ""},      // no plain fallback for nerd-only names
		{"nope", false, ""},
		{"nope", true, ""},
	}
	for _, tc := range cases {
		if got := Icon(tc.name, tc.nerd); got != tc.want {
			t.Errorf("Icon(%q, %v) = %q, want %q", tc.name, tc.nerd, got, tc.want)
		}
	}
}
