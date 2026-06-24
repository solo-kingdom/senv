package tui

import tea "github.com/charmbracelet/bubbletea"

// Tab is the interface implemented by each tab view (Env / Text / Config).
//
// Each tab owns its own navigation state (selected group, selected item,
// filter, etc.) so that switching tabs and back preserves the cursor.
type Tab interface {
	// Title returns the label shown in the tab strip.
	Title() string

	// Help returns the keybinding hint shown in the status bar.
	Help() string

	// InputMode reports whether the tab is currently capturing text input
	// (e.g. an inline edit modal or filter box). When true, the top-level
	// model forwards ALL key messages to the tab instead of intercepting
	// global shortcuts (q, 1/2/3, Tab).
	InputMode() bool

	// Init performs initial command setup for the tab.
	Init() tea.Cmd

	// Update handles a message and returns the (possibly mutated) tab.
	Update(msg tea.Msg) (Tab, tea.Cmd)

	// View renders the tab content (without the tab strip / status bar).
	View() string

	// SetSize informs the tab of the available content area size.
	SetSize(width, height int)
}
