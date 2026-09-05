package app

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/wrongstack/wrongtop/internal/ui"
)

// helpSection is one titled group of "key  description" lines in the
// help overlay.
type helpSection struct {
	title string
	lines []string
}

// helpSections builds the help overlay content for the current config so
// overridden key bindings render as configured. Each line is "key  desc";
// helpView splits on the first double space. Sections for disabled
// modules are omitted.
func (m *Model) helpSections() []helpSection {
	secs := []helpSection{
		{
			title: "GLOBAL",
			lines: []string{
				fmt.Sprintf("1-%d        jump to tab", len(m.tabs)),
				"tab        next tab (shift+tab back)",
				"p          dashboard density preset",
				"T          cycle color theme",
				"R          reload config file",
				"a          alert history",
				"q / ctrl+c quit",
				"?          toggle this help",
				"mouse      scroll and click lists",
			},
		},
		{
			title: "DISKS",
			lines: []string{
				"left/right switch usage / I/O table",
				"up/down    move selection",
			},
		},
	}
	if m.cfg.Modules.Processes {
		secs = append(secs, helpSection{
			title: "PROCESSES",
			lines: []string{
				m.cfg.Keys.Filter + "          filter by name, user or pid",
				"s          cycle sort column",
				"S          reverse sort order",
				"t          toggle process tree",
				"left/right collapse / expand tree",
				"enter      process details",
				m.killKey() + "          signal menu (SIGTERM)",
				m.forceKey() + "          signal menu (SIGKILL)",
				"←→         pick signal · y sends",
				"up/down    move selection",
			},
		})
	}
	if m.cfg.Modules.Docker {
		secs = append(secs, helpSection{
			title: "DOCKER",
			lines: []string{
				"enter      follow container logs",
				"s          start container",
				"t          stop container",
				"r          restart container",
				"esc        back from logs",
			},
		})
	}
	return secs
}

// killKey returns the configured terminate key, lowercased; its uppercase
// variant force-kills.
func (m *Model) killKey() string { return strings.ToLower(m.cfg.Keys.Kill) }

// forceKey returns the force-kill variant of the terminate key.
func (m *Model) forceKey() string { return strings.ToUpper(m.killKey()) }

// helpView renders the help overlay box: two columns so everything fits
// without scrolling on smaller terminals.
func (m *Model) helpView() string {
	st := m.theme.Styles
	secs := m.helpSections()
	render := func(secs []helpSection) string {
		var b strings.Builder
		for i, sec := range secs {
			if i > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(st.Title.Render(sec.title))
			for _, l := range sec.lines {
				key, desc, _ := strings.Cut(l, "  ")
				key = strings.TrimSpace(key)
				desc = strings.TrimSpace(desc)
				// pad by display width: byte padding shoves the whole
				// column around whenever a key is multi-byte ("←")
				b.WriteString("\n  " + st.HelpKey.Render(pad(key, 10)) + st.HelpText.Render(desc))
			}
		}
		return b.String()
	}
	split := (len(secs) + 1) / 2
	left := render(secs[:split])
	right := render(secs[split:])
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		left,
		strings.Repeat(" ", 6),
		right,
	)
	b := &strings.Builder{}
	b.WriteString(body)
	b.WriteString("\n\n  " + st.HelpKey.Render("esc") + st.HelpText.Render(" close"))
	return ui.Box(ui.BorderFor(m.cfg.Border), st.Border, st.BorderChar, st.BorderTitle,
		"WRONGTOP HELP", b.String())
}

// pad right-pads to n display cells (runes, not bytes).
func pad(s string, n int) string {
	w := lipgloss.Width(s)
	for w < n {
		s += " "
		w++
	}
	return s
}
