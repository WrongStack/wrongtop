package app

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/ersinkoc/wrongtop/internal/ui"
)

// helpSections is the full help overlay content, grouped per tab.
var helpSections = []struct {
	title string
	lines []string
}{
	{
		title: "GLOBAL",
		lines: []string{
			"1-5        jump to tab",
			"tab        next tab (shift+tab back)",
			"q / ctrl+c quit",
			"?          toggle this help",
			"mouse      scroll lists",
		},
	},
	{
		title: "PROCESSES",
		lines: []string{
			"/          filter by name, user or pid",
			"s          cycle sort column",
			"S          reverse sort order",
			"k          terminate (SIGTERM)",
			"K          force kill (SIGKILL)",
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

// helpView renders the help overlay box.
func (m *Model) helpView() string {
	st := m.theme.Styles
	var b strings.Builder
	for i, sec := range helpSections {
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
