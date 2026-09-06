// Package ui defines the shared tab contract and helpers used by all
// wrongtop tabs.
package ui

import (
	"charm.land/bubbletea/v2"

	"github.com/wrongstack/wrongtop/internal/theme"
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
	// SetTheme swaps the color theme without losing tab state.
	SetTheme(th *theme.Theme)
	// SetVisible marks the tab as the active one. Snapshots still reach
	// hidden tabs (history buffers stay warm), but heavy per-snapshot
	// work — table rebuilds above all — may be skipped while hidden,
	// provided SetVisible(true) catches up from the latest data.
	SetVisible(visible bool)
}
