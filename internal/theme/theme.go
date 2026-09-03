// Package theme holds the color palettes and lipgloss styles used across
// wrongtop.
package theme

import (
	"charm.land/lipgloss/v2"
)

// Palette is a named set of terminal colors.
type Palette struct {
	Name string
	BG   string
	FG   string

	Red    string
	Green  string
	Yellow string
	Blue   string
	Purple string
	Cyan   string
	Orange string
	Gray   string
}

// Built-in palettes.
var (
	// GruvboxDark is the default wrongtop palette.
	GruvboxDark = Palette{
		Name:   "gruvbox-dark",
		BG:     "#282828",
		FG:     "#ebdbb2",
		Red:    "#fb4934",
		Green:  "#b8bb26",
		Yellow: "#fabd2f",
		Blue:   "#83a598",
		Purple: "#d3869b",
		Cyan:   "#8ec07c",
		Orange: "#fe8019",
		Gray:   "#928374",
	}

	CatppuccinMocha = Palette{
		Name:   "catppuccin-mocha",
		BG:     "#1e1e2e",
		FG:     "#cdd6f4",
		Red:    "#f38ba8",
		Green:  "#a6e3a1",
		Yellow: "#f9e2af",
		Blue:   "#89b4fa",
		Purple: "#cba6f7",
		Cyan:   "#94e2d5",
		Orange: "#fab387",
		Gray:   "#6c7086",
	}

	Dracula = Palette{
		Name:   "dracula",
		BG:     "#282a36",
		FG:     "#f8f8f2",
		Red:    "#ff5555",
		Green:  "#50fa7b",
		Yellow: "#f1fa8c",
		Blue:   "#8be9fd",
		Purple: "#bd93f9",
		Cyan:   "#8be9fd",
		Orange: "#ffb86c",
		Gray:   "#6272a4",
	}
)

var registry = map[string]Palette{
	GruvboxDark.Name:     GruvboxDark,
	CatppuccinMocha.Name: CatppuccinMocha,
	Dracula.Name:         Dracula,
}

// PaletteNames lists the built-in palette names in display order.
func PaletteNames() []string {
	return []string{GruvboxDark.Name, CatppuccinMocha.Name, Dracula.Name}
}

// Theme pairs a palette with the derived lipgloss styles.
type Theme struct {
	Palette Palette
	Styles  Styles
}

// Styles are the shared lipgloss styles derived from a palette.
type Styles struct {
	TabBar       lipgloss.Style
	TabActive    lipgloss.Style
	TabInactive  lipgloss.Style
	Status       lipgloss.Style
	Title        lipgloss.Style
	HelpKey      lipgloss.Style
	HelpText     lipgloss.Style
	OK           lipgloss.Style
	Warn         lipgloss.Style
	Crit         lipgloss.Style
	Muted        lipgloss.Style
	Border       lipgloss.Style
	BorderTitle  lipgloss.Style
	Placeholder  lipgloss.Style
	PlaceholderN lipgloss.Style
}

// ByName resolves a theme by name, falling back to the default.
func ByName(name string) *Theme {
	p, ok := registry[name]
	if !ok {
		p = GruvboxDark
	}
	return New(p)
}

// New derives a full theme from a palette.
func New(p Palette) *Theme {
	c := lipgloss.Color
	s := Styles{
		TabBar: lipgloss.NewStyle().
			Background(c(p.BG)).
			Foreground(c(p.FG)),
		TabActive: lipgloss.NewStyle().
			Background(c(p.Purple)).
			Foreground(c(p.BG)).
			Bold(true).
			Padding(0, 1),
		TabInactive: lipgloss.NewStyle().
			Background(c(p.BG)).
			Foreground(c(p.Gray)).
			Padding(0, 1),
		Status: lipgloss.NewStyle().
			Background(c(p.BG)).
			Foreground(c(p.FG)),
		Title: lipgloss.NewStyle().
			Foreground(c(p.Purple)).
			Bold(true),
		HelpKey: lipgloss.NewStyle().
			Foreground(c(p.Cyan)),
		HelpText: lipgloss.NewStyle().
			Foreground(c(p.Gray)),
		OK:    lipgloss.NewStyle().Foreground(c(p.Green)),
		Warn:  lipgloss.NewStyle().Foreground(c(p.Yellow)),
		Crit:  lipgloss.NewStyle().Foreground(c(p.Red)).Bold(true),
		Muted: lipgloss.NewStyle().Foreground(c(p.Gray)),
		Border: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(c(p.Blue)),
		BorderTitle: lipgloss.NewStyle().
			Foreground(c(p.Blue)).
			Bold(true),
		Placeholder: lipgloss.NewStyle().
			Foreground(c(p.FG)).
			Bold(true),
		PlaceholderN: lipgloss.NewStyle().
			Foreground(c(p.Gray)),
	}
	return &Theme{Palette: p, Styles: s}
}

// Value colors a percentage according to the configured thresholds.
func (t *Theme) Value(warn, crit, pct float64) lipgloss.Style {
	switch {
	case pct >= crit:
		return t.Styles.Crit
	case pct >= warn:
		return t.Styles.Warn
	default:
		return t.Styles.OK
	}
}
