package ui

// Icons name the glyphs used across the chrome: tab titles, panel
// titles, chips. The plain set is Unicode available in every terminal;
// the nerd set uses Font Awesome code points for users with
// nerd_fonts: true.
var (
	plainIcons = map[string]string{
		"host":   "⌂",
		"cpu":    "⚡",
		"mem":    "▦",
		"net":    "⇅",
		"disk":   "▤",
		"gpu":    "◆",
		"proc":   "☰",
		"docker": "▣",
		"warn":   "⚠",
		"bat":    "⚡",
		"bolt":   "⚡",
		"sensor": "♨",
		"conn":   "⇄",
		"fan":    "❉",
	}
	nerdIcons = map[string]string{
		"host":   "\uf015", // home
		"cpu":    "\uf2db", // microchip
		"mem":    "\uf538", // memory
		"net":    "\uf6ff", // network-wired
		"disk":   "\uf0a0", // hdd
		"gpu":    "\uf108", // desktop (display adapter)
		"proc":   "\uf03a", // list
		"docker": "\uf395", // docker
		"warn":   "\uf071", // exclamation-triangle
		"bat":    "\uf240", // battery-full
		"bolt":   "\uf0e7", // bolt (charging)
		"temp":   "\uf2c9", // thermometer
		"sensor": "\uf2c9", // thermometer
		"conn":   "\uf362", // exchange
		"fan":    "\uf863", // fan
	}
)

// Icon returns the glyph for a named UI role. Unknown names yield "".
func Icon(name string, nerd bool) string {
	if nerd {
		if g, ok := nerdIcons[name]; ok {
			return g
		}
	}
	return plainIcons[name]
}
