// Package ui defines the shared tab contract and helpers used by all
// wrongtop tabs.
package ui

import (
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Tab is a single page of the wrongtop UI. The root model forwards
// messages to tabs; tabs keep their own state (history buffers, cursor
// position, selection) so switching tabs is lossless.
type Tab interface {
	// Title is the label shown in the tab bar.
	Title() string
	// SetSize receives the current content area dimensions on startup
	// and on every terminal resize.
	SetSize(width, height int)
	// Update handles a message and may return a command.
	Update(msg tea.Msg) tea.Cmd
	// View renders the tab inside the content area.
	View() string
}

// Placeholder is a stand-in tab for features that are not wired up yet.
type Placeholder struct {
	title string
	note  string
	width int
}

// NewPlaceholder returns a placeholder tab.
func NewPlaceholder(title, note string) *Placeholder {
	return &Placeholder{title: title, note: note}
}

// Title implements Tab.
func (p *Placeholder) Title() string { return p.title }

// SetSize implements Tab.
func (p *Placeholder) SetSize(width, height int) { p.width = width }

// Update implements Tab; placeholders have no state.
func (p *Placeholder) Update(tea.Msg) tea.Cmd { return nil }

// View implements Tab.
func (p *Placeholder) View() string {
	body := lipgloss.JoinVertical(lipgloss.Center,
		lipgloss.NewStyle().Bold(true).Render(p.title),
		lipgloss.NewStyle().Faint(true).Render(p.note),
	)
	return lipgloss.Place(p.width, 0, lipgloss.Center, lipgloss.Center, body)
}
