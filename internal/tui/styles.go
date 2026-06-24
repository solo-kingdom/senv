package tui

import "github.com/charmbracelet/lipgloss"

// Color palette
const (
	colorAccent  = "99"  // purple
	colorActive  = "213" // pink
	colorMuted   = "241" // gray
	colorError   = "196" // red
	colorSuccess = "46"  // green
	colorWarn    = "214" // orange
)

// Styles holds all lipgloss styles used by the TUI.
var (
	// frameStyle is the outer rounded border that wraps the whole TUI,
	// forming a single application window. Width/Height are set at render
	// time in View() so the frame always fills the terminal exactly.
	frameStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(0, 0)

	// tabStripStyle is the container for the top tab strip row. It carries
	// a muted bottom border that visually separates the tab strip from the
	// content area. Width is set at render time to the inner content width.
	// Border(NormalBorder, top,right,bottom,left) = bottom-only.
	tabStripStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(lipgloss.Color(colorMuted)).
			Padding(0, 1)

	// paneTitleStyle is the small in-pane heading rendered inside a column
	// (e.g. "Groups (n)" or a list header) — not the tab strip container.
	paneTitleStyle = lipgloss.NewStyle().
			Padding(0, 1)

	// tabStyle is an inactive tab label: muted foreground, no background.
	tabStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color(colorMuted))

	// activeTabStyle is the currently selected tab label: accent background
	// block with white bold text, so the active tab reads as a raised tab.
	activeTabStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Background(lipgloss.Color(colorAccent)).
			Foreground(lipgloss.Color("#ffffff")).
			Bold(true)

	// tabSeparatorStyle renders the vertical separator between tab labels.
	tabSeparatorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorMuted))

	// statusBarStyle is the bottom help/status bar.
	statusBarStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color(colorMuted))

	// errorBarStyle renders error messages at the bottom.
	errorBarStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color(colorError))

	// contentStyle wraps the main content area.
	contentStyle = lipgloss.NewStyle().Padding(0, 1)

	// paneStyle is a generic pane (left/right column) style.
	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1)

	// activePaneStyle highlights the currently focused pane.
	activePaneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(colorAccent)).
			Padding(0, 1)

	// selectedLineStyle highlights the cursor line in a list.
	selectedLineStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color(colorAccent))

	// maskedValueStyle renders masked secrets distinctly.
	maskedValueStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorWarn))

	// searchOverlayStyle wraps the global search overlay.
	searchOverlayStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color(colorAccent)).
				Padding(1, 2)

	// typeBadgeStyles map a result type to a colored badge.
	typeBadgeStyles = map[string]lipgloss.Style{
		typeEnv:    lipgloss.NewStyle().Foreground(lipgloss.Color("39")),  // blue
		typeText:   lipgloss.NewStyle().Foreground(lipgloss.Color("48")),  // green
		typeConfig: lipgloss.NewStyle().Foreground(lipgloss.Color("215")), // orange
	}

	// emptyStateStyle renders empty-state hints.
	emptyStateStyle = lipgloss.NewStyle().
			Italic(true).
			Foreground(lipgloss.Color(colorMuted)).
			Padding(1, 2)
)
