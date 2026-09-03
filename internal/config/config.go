// Package config loads and represents the wrongtop user configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

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
	Modules    Modules    `yaml:"modules"`
	Thresholds Thresholds `yaml:"thresholds"`
	Keys       Keys       `yaml:"keys"`
}

// Modules toggles optional feature tabs.
type Modules struct {
	Docker    bool `yaml:"docker"`
	Processes bool `yaml:"processes"`
}

// Thresholds controls when metric values change from normal to warning to
// critical colors.
type Thresholds struct {
	CPUWarn float64 `yaml:"cpu_warn"`
	CPUCrit float64 `yaml:"cpu_crit"`
	MemWarn float64 `yaml:"mem_warn"`
	MemCrit float64 `yaml:"mem_crit"`
}

// Keys overrides default key bindings.
type Keys struct {
	Kill   string `yaml:"kill"`
	Filter string `yaml:"filter"`
}

// Default returns the built-in configuration.
func Default() *Config {
	return &Config{
		Theme:   "gruvbox-dark",
		Refresh: Duration(time.Second),
		Modules: Modules{Docker: true, Processes: true},
		Thresholds: Thresholds{
			CPUWarn: 70, CPUCrit: 90,
			MemWarn: 80, MemCrit: 95,
		},
		Keys: Keys{Kill: "k", Filter: "/"},
	}
}

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
		c.Theme = "gruvbox-dark"
	}
}
