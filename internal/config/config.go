// Package config loads and represents the wrongtop user configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Duration wraps time.Duration so it can round-trip through YAML as a
// human-friendly string such as "1s" or "500ms".
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q", s)
	}
	*d = Duration(v)
	return nil
}

// D returns the underlying time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

// Config is the top-level wrongtop configuration.
type Config struct {
	Theme      string     `yaml:"theme"`
	Refresh    Duration   `yaml:"refresh"`
	Layout     string     `yaml:"layout"`  // full | compact | minimal
	Modules    Modules    `yaml:"modules"` //nolint:gocritic // grouped YAML namespace
	Thresholds Thresholds `yaml:"thresholds"`
	Keys       Keys       `yaml:"keys"`
	ReadOnly   bool       `yaml:"read_only,omitempty"` // no process signaling
	NerdFonts  bool       `yaml:"nerd_fonts,omitempty"`
	Border     string     `yaml:"border,omitempty"` // rounded | square panel corners
}

// Modules toggles optional feature tabs.
type Modules struct {
	Docker      bool `yaml:"docker"`
	Processes   bool `yaml:"processes"`
	Sensors     bool `yaml:"sensors"`     // temperatures, fans, battery
	Connections bool `yaml:"connections"` // live TCP table
}

// Thresholds controls when metric values change from normal to warning to
// critical colors.
type Thresholds struct {
	CPUWarn  float64 `yaml:"cpu_warn"`
	CPUCrit  float64 `yaml:"cpu_crit"`
	MemWarn  float64 `yaml:"mem_warn"`
	MemCrit  float64 `yaml:"mem_crit"`
	TempWarn float64 `yaml:"temp_warn"` // °C
	TempCrit float64 `yaml:"temp_crit"` // °C
}

// Keys overrides default key bindings.
type Keys struct {
	Kill   string `yaml:"kill"`
	Filter string `yaml:"filter"`
}

// Default returns the built-in configuration.
func Default() *Config {
	return &Config{
		Theme:   "tokyo-night",
		Refresh: Duration(time.Second),
		Modules: Modules{Docker: true, Processes: true, Sensors: true, Connections: true},
		Thresholds: Thresholds{
			CPUWarn: 70, CPUCrit: 90,
			MemWarn: 80, MemCrit: 95,
			TempWarn: 60, TempCrit: 80,
		},
		Keys: Keys{Kill: "k", Filter: "/"},
	}
}

// Sample is an annotated example configuration, printed by
// `wrongtop config-sample`.
const Sample = `# ~/.config/wrongtop/config.yaml
theme: tokyo-night       # 10 built-ins, or a file in ~/.config/wrongtop/themes
refresh: 1s              # min 250ms, max 10s
layout: full             # full | compact | minimal (p cycles it live)
nerd_fonts: false        # true: icon glyphs + powerline separators in tabs, panels, status bar
border: rounded          # rounded | square | thick | double panel corners

modules:
  docker: true           # show the DOCKER tab (daemon optional)
  processes: true
  sensors: true          # show the SENSORS tab (temps, fans, battery)
  connections: true      # show the CONNECTIONS tab (live TCP table)
thresholds:              # percent → warn/critical coloring
  cpu_warn: 70
  cpu_crit: 90
  mem_warn: 80
  mem_crit: 95
  temp_warn: 60           # CPU temperature °C (shown when the platform reports it)
  temp_crit: 80

keys:                    # overrides (processes tab)
  kill: k                # open the signal menu
  filter: /
`

// Path returns the configuration file location: $WRONGTOP_CONFIG if set,
// otherwise ~/.config/wrongtop/config.yaml.
func Path() string {
	if p := os.Getenv("WRONGTOP_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "wrongtop", "config.yaml")
}

// Load reads configuration from path. An empty path means the default
// location; a missing file yields the defaults.
func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		path = Path()
	}
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("wrongtop: reading config: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("wrongtop: parsing %s: %w", path, err)
	}
	cfg.normalize()
	return cfg, nil
}

// normalize clamps values into sane ranges.
func (c *Config) normalize() {
	if c.Refresh.D() < 250*time.Millisecond {
		c.Refresh = Duration(250 * time.Millisecond)
	}
	if c.Refresh.D() > 10*time.Second {
		c.Refresh = Duration(10 * time.Second)
	}
	if c.Theme == "" {
		c.Theme = "tokyo-night"
	}
	switch c.Layout {
	case "full", "compact", "minimal":
	default:
		c.Layout = "full"
	}
	switch c.Border {
	case "rounded", "square", "thick", "double":
	default:
		c.Border = "rounded"
	}
	if c.Thresholds.TempWarn <= 0 {
		c.Thresholds.TempWarn = 60
	}
	if c.Thresholds.TempCrit <= c.Thresholds.TempWarn {
		c.Thresholds.TempCrit = 80
	}
	if c.Thresholds.CPUWarn <= 0 {
		c.Thresholds.CPUWarn = 70
	}
	if c.Thresholds.CPUCrit <= c.Thresholds.CPUWarn {
		c.Thresholds.CPUCrit = 90
	}
	if c.Thresholds.MemWarn <= 0 {
		c.Thresholds.MemWarn = 80
	}
	if c.Thresholds.MemCrit <= c.Thresholds.MemWarn {
		c.Thresholds.MemCrit = 95
	}
	if !validKeyOverride(c.Keys.Kill) {
		c.Keys.Kill = "k"
	}
	if !validKeyOverride(c.Keys.Filter) {
		c.Keys.Filter = "/"
	}
	// The kill key is matched case-insensitively and its uppercase variant
	// force-kills, so kill and filter must not collide.
	if strings.EqualFold(c.Keys.Kill, c.Keys.Filter) {
		c.Keys.Kill = "k"
		c.Keys.Filter = "/"
	}
}

// validKeyOverride reports whether s is a usable single-letter key
// binding. Multi-character values ("ctrl+k"), non-letters ("1", "#") and
// the reserved sort keys ("s", "S") are rejected so they fall back to the
// defaults instead of shadowing built-in bindings.
func validKeyOverride(s string) bool {
	r := []rune(s)
	if len(r) != 1 || !unicode.IsLetter(r[0]) {
		return false
	}
	switch r[0] {
	case 's', 'S': // reserved: sort cycle / reverse order
		return false
	case 't', 'T': // reserved: tree toggle; the key switch matches "t"
		return false // before the configured kill/filter cases
	case 'a', 'A': // reserved: alerts overlay; globalKey consumes "a"
		return false // before the tabs dispatch
	case 'q', 'Q': // reserved: quit; globalKey consumes "q"
		return false
	case 'r', 'R': // reserved: live config reload; globalKey consumes "R"
		return false
	}
	return true
}
