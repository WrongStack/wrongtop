// Package theme holds the color palettes and lipgloss styles used across
// wrongtop: ten built-in palettes plus user-defined palettes loaded
// from ~/.config/wrongtop/themes/*.yml.
package theme

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"gopkg.in/yaml.v3"

	"github.com/wrongstack/wrongtop/internal/ui/canvas"
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
	// GruvboxDark seeds the unset slots of user-defined palettes.
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
		// the dracula spec has no blue between cyan and purple; the magenta
		// accent (#ff79c6) fills the slot so panel borders stay distinct
		Blue:   "#ff79c6",
		Purple: "#bd93f9",
		Cyan:   "#8be9fd",
		Orange: "#ffb86c",
		Gray:   "#6272a4",
	}

	Nord = Palette{
		Name:   "nord",
		BG:     "#2e3440",
		FG:     "#eceff4",
		Red:    "#bf616a",
		Green:  "#a3be8c",
		Yellow: "#ebcb8b",
		Blue:   "#81a1c1",
		Purple: "#b48ead",
		Cyan:   "#88c0d0",
		Orange: "#d08770",
		Gray:   "#7b88a1",
	}

	TokyoNight = Palette{
		Name:   "tokyo-night",
		BG:     "#1a1b26",
		FG:     "#c0caf5",
		Red:    "#f7768e",
		Green:  "#9ece6a",
		Yellow: "#e0af68",
		Blue:   "#7aa2f7",
		Purple: "#bb9af7",
		Cyan:   "#7dcfff",
		Orange: "#ff9e64",
		Gray:   "#565f89",
	}

	SolarizedDark = Palette{
		Name:   "solarized-dark",
		BG:     "#002b36",
		FG:     "#eee8d5",
		Red:    "#dc322f",
		Green:  "#859900",
		Yellow: "#b58900",
		Blue:   "#268bd2",
		Purple: "#d33682",
		Cyan:   "#2aa198",
		Orange: "#cb4b16",
		Gray:   "#93a1a1",
	}

	OneDark = Palette{
		Name:   "one-dark",
		BG:     "#282c34",
		FG:     "#abb2bf",
		Red:    "#e06c75",
		Green:  "#98c379",
		Yellow: "#e5c07b",
		Blue:   "#61afef",
		Purple: "#c678dd",
		Cyan:   "#56b6c2",
		Orange: "#d19a66",
		Gray:   "#5c6370",
	}

	RosePine = Palette{
		Name:   "rose-pine",
		BG:     "#191724",
		FG:     "#e0def4",
		Red:    "#eb6f92",
		Green:  "#31748f",
		Yellow: "#f6c177",
		Blue:   "#9ccfd8",
		Purple: "#c4a7e7",
		Cyan:   "#ebbcba",
		// the base rose-pine accents run out after cyan, so the orange
		// slot borrows dawn's rose (#d7827e) to stay distinct from cyan
		Orange: "#d7827e",
		Gray:   "#6e6a86",
	}

	EverforestDark = Palette{
		Name:   "everforest-dark",
		BG:     "#2f383e",
		FG:     "#d3c6aa",
		Red:    "#e67e80",
		Green:  "#a7c080",
		Yellow: "#dbbc7f",
		Blue:   "#7fbbb3",
		Purple: "#d699b6",
		Cyan:   "#83c092",
		Orange: "#e69875",
		Gray:   "#9da9a0",
	}

	Kanagawa = Palette{
		Name:   "kanagawa",
		BG:     "#1f1f28",
		FG:     "#dcd7ba",
		Red:    "#e46876",
		Green:  "#98bb6c",
		Yellow: "#c0a36e",
		Blue:   "#7e9cd8",
		Purple: "#957fb8",
		Cyan:   "#7aa89f",
		Orange: "#ffa066",
		Gray:   "#727169",
	}
)

var registry = map[string]Palette{
	GruvboxDark.Name:     GruvboxDark,
	CatppuccinMocha.Name: CatppuccinMocha,
	Dracula.Name:         Dracula,
	Nord.Name:            Nord,
	TokyoNight.Name:      TokyoNight,
	SolarizedDark.Name:   SolarizedDark,
	OneDark.Name:         OneDark,
	RosePine.Name:        RosePine,
	EverforestDark.Name:  EverforestDark,
	Kanagawa.Name:        Kanagawa,
}

// PaletteNames lists the built-in palette names in display order.
func PaletteNames() []string {
	return []string{
		GruvboxDark.Name, CatppuccinMocha.Name, Dracula.Name,
		Nord.Name, TokyoNight.Name, SolarizedDark.Name,
		OneDark.Name, RosePine.Name, EverforestDark.Name, Kanagawa.Name,
	}
}

// userPalette is the YAML shape of a theme file: eleven hex colors.
type userPalette struct {
	Name   string `yaml:"name"`
	BG     string `yaml:"bg"`
	FG     string `yaml:"fg"`
	Red    string `yaml:"red"`
	Green  string `yaml:"green"`
	Yellow string `yaml:"yellow"`
	Blue   string `yaml:"blue"`
	Purple string `yaml:"purple"`
	Cyan   string `yaml:"cyan"`
	Orange string `yaml:"orange"`
	Gray   string `yaml:"gray"`
}

var (
	userMu       sync.Mutex
	userPalettes map[string]Palette
	userLoaded   bool
)

// ThemesDir returns the directory user theme files are loaded from:
// ~/.config/wrongtop/themes.
func ThemesDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "wrongtop", "themes")
}

// LoadUserThemes (re)reads every *.yml file in dir into the user
// palette registry. Files whose name does not start with '#' inherit
// the file name as the palette name; incomplete colors fall back to the
// default palette for that slot.
func LoadUserThemes(dir string) error {
	userMu.Lock()
	defer userMu.Unlock()
	return loadUserThemes(dir)
}

// loadUserThemes is the lock-free body of LoadUserThemes; callers must
// hold userMu.
func loadUserThemes(dir string) error {
	userPalettes = map[string]Palette{}
	userLoaded = true
	if dir == "" {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.yml"))
	if err != nil {
		return err
	}
	for _, file := range matches {
		raw, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var up userPalette
		if err := yaml.Unmarshal(raw, &up); err != nil {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(file), ".yml")
		if up.Name != "" {
			name = up.Name
		}
		p := GruvboxDark // slot defaults
		p.Name = name
		for _, slot := range []struct {
			hex string
			dst *string
		}{
			{up.BG, &p.BG}, {up.FG, &p.FG}, {up.Red, &p.Red}, {up.Green, &p.Green},
			{up.Yellow, &p.Yellow}, {up.Blue, &p.Blue}, {up.Purple, &p.Purple},
			{up.Cyan, &p.Cyan}, {up.Orange, &p.Orange}, {up.Gray, &p.Gray},
		} {
			if slot.hex != "" {
				*slot.dst = slot.hex
			}
		}
		userPalettes[name] = p
	}
	return nil
}

// userRegistry returns the user palettes, loading them on first use.
func userRegistry() map[string]Palette {
	userMu.Lock()
	defer userMu.Unlock()
	if !userLoaded {
		_ = loadUserThemes(ThemesDir())
	}
	return userPalettes
}

// Names lists every selectable theme: built-ins first, then user themes
// alphabetically.
func Names() []string {
	names := PaletteNames()
	user := userRegistry()
	var extra []string
	for name := range user {
		extra = append(extra, name)
	}
	sort.Strings(extra)
	return append(names, extra...)
}

// Resolve finds a palette by name: user themes first (so a file can
// override a built-in), then the built-in registry, else the default.
func Resolve(name string) (Palette, bool) {
	if p, ok := userRegistry()[name]; ok {
		return p, true
	}
	p, ok := registry[name]
	return p, ok
}

// ByName resolves a theme by name, falling back to the default
// (tokyo-night, matching config.Default).
func ByName(name string) *Theme {
	p, ok := Resolve(name)
	if !ok {
		p = TokyoNight
	}
	return New(p)
}

// Soft blends a palette color toward the panel background — solid
// primary colors read harsh in large fills, so chips, active tabs and
// alert strips use this muted mix instead (modern soft-UI look).
func (t *Theme) Soft(hex string) string {
	return canvas.Ramp{t.Palette.BG, hex}.At(0.55)
}

// Theme pairs a palette with the derived lipgloss styles.
type Theme struct {
	Palette Palette
	Styles  Styles
	Ramps   Ramps
}

// Styles are the shared lipgloss styles derived from a palette.
type Styles struct {
	TabBar      lipgloss.Style
	TabActive   lipgloss.Style
	TabInactive lipgloss.Style
	Status      lipgloss.Style
	Title       lipgloss.Style
	FG          lipgloss.Style // plain readable text: never left to the terminal default
	HelpKey     lipgloss.Style
	HelpText    lipgloss.Style
	OK          lipgloss.Style
	Warn        lipgloss.Style
	Crit        lipgloss.Style
	Muted       lipgloss.Style
	Track       lipgloss.Style // the empty run of a meter: fainter than Muted text
	Border      lipgloss.Style
	BorderChar  lipgloss.Style
	BorderTitle lipgloss.Style
	Selected    lipgloss.Style // focused row of the data tables
}

// Ramps groups the value→color gradients shared by every tab, derived
// from the palette so a metric reads the same wherever it appears.
type Ramps struct {
	CPU  canvas.Ramp // utilization, low → high: green → yellow → orange → red
	Mem  canvas.Ramp // memory pressure: blue → purple → red
	Swap canvas.Ramp // swap usage: purple → red
	IO   canvas.Ramp // disk activity: cyan → green → yellow → red
	Bat  canvas.Ramp // battery, empty → full: red → orange → green
	Load canvas.Ramp // load average: cyan → green
	RX   canvas.Ramp // download share of the mixed meters: green → cyan
	TX   canvas.Ramp // upload share of the mixed meters: blue → cyan
}

// New derives a full theme from a palette.
func New(p Palette) *Theme {
	c := lipgloss.Color
	s := Styles{
		TabBar: lipgloss.NewStyle().
			Background(c(p.BG)).
			Foreground(c(p.FG)),
		TabActive: lipgloss.NewStyle().
			Background(c(canvas.Ramp{p.BG, p.Purple}.At(0.55))).
			Foreground(c(p.FG)).
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
		FG: lipgloss.NewStyle().
			Foreground(c(p.FG)),
		HelpKey: lipgloss.NewStyle().
			Foreground(c(p.Cyan)),
		HelpText: lipgloss.NewStyle().
			Foreground(c(p.Gray)),
		OK:    lipgloss.NewStyle().Foreground(c(p.Green)),
		Warn:  lipgloss.NewStyle().Foreground(c(p.Yellow)),
		Crit:  lipgloss.NewStyle().Foreground(c(p.Red)).Bold(true),
		Muted: lipgloss.NewStyle().Foreground(c(p.Gray)),
		Track: lipgloss.NewStyle().
			Foreground(c(canvas.Ramp{p.BG, p.Gray}.At(0.45))),
		Border: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(c(p.Blue)),
		BorderChar: lipgloss.NewStyle().
			Foreground(c(p.Blue)),
		BorderTitle: lipgloss.NewStyle().
			Foreground(c(p.Blue)).
			Bold(true),
		Selected: lipgloss.NewStyle().
			Background(c(canvas.Ramp{p.BG, p.Purple}.At(0.30))).
			Foreground(c(p.FG)).
			Bold(true),
	}
	r := Ramps{
		CPU:  canvas.Ramp{p.Green, p.Yellow, p.Orange, p.Red},
		Mem:  canvas.Ramp{p.Blue, p.Purple, p.Red},
		Swap: canvas.Ramp{p.Purple, p.Red},
		IO:   canvas.Ramp{p.Cyan, p.Green, p.Yellow, p.Red},
		Bat:  canvas.Ramp{p.Red, p.Orange, p.Green},
		Load: canvas.Ramp{p.Cyan, p.Green},
		RX:   canvas.Ramp{p.Green, p.Cyan},
		TX:   canvas.Ramp{p.Blue, p.Cyan},
	}
	return &Theme{Palette: p, Styles: s, Ramps: r}
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
