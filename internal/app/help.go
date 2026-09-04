package app

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/ersinkoc/wrongtop/internal/ui"
)

// helpSection is one titled group of "key  description" lines in the
// help overlay.
type helpSection struct {
	title string
	lines []string
}

// helpSections builds the help overlay content for the current config so
// overridden key bindings render as configured. Each line is "key  desc";
// helpView splits on the first double space.
func (m *Model) helpSections() []helpSection {
	return []helpSection{
		{
			title: "GLOBAL",
			lines: []string{
				"1-5        jump to tab",
				"tab        next tab (shift+tab back)",
				"T          cycle color theme",
				"R          reload config file",
				"a          alert history",
				"q / ctrl+c quit",
				"?          toggle this help",
				"mouse      scroll and click lists",
			},
		},
		{
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
		},
		{
			title: "DOCKER",
			lines: []string{
				"enter      follow container logs",
				"s          start container",
				"t          stop container",
				"r          restart container",
				"esc        back from logs",
			},
		},
	}
}

// killKey returns the configured terminate key, lowercased; its uppercase
// variant force-kills.
func (m *Model) killKey() string { return strings.ToLower(m.cfg.Keys.Kill) }

// forceKey returns the force-kill variant of the terminate key.
func (m *Model) forceKey() string { return strings.ToUpper(m.killKey()) }

// helpView renders the help overlay box.
func (m *Model) helpView() string {
	st := m.theme.Styles
	var b strings.Builder
	for i, sec := range m.helpSections() {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(st.Title.Render(sec.title))
		for _, l := range sec.lines {
			key, desc, _ := strings.Cut(l, "  ")
			key = strings.TrimSpace(key)
			desc = strings.TrimSpace(desc)
			b.WriteString("\n  " + st.HelpKey.Render(pad(key, 10)) + st.HelpText.Render(desc))
		}
	}
	b.WriteString("\n\n  " + st.HelpKey.Render("esc") + st.HelpText.Render(" close"))
	return ui.Box(lipgloss.RoundedBorder(), st.Border, st.BorderChar, st.BorderTitle,
		"WRONGTOP HELP", b.String())
}

func pad(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}
