package tui

import "github.com/charmbracelet/lipgloss"

// Adaptive color palette for light/dark terminals (Tokyo Night-ish blue accent).
// Using AdaptiveColor: first value is dark terminal, second is light terminal.
var (
	// Core palette roles
	colorFg     = lipgloss.AdaptiveColor{Dark: "#c0caf5", Light: "#343b58"}
	colorMuted  = lipgloss.AdaptiveColor{Dark: "#565f89", Light: "#9699a3"}
	colorDim    = lipgloss.AdaptiveColor{Dark: "#414868", Light: "#b4b5b9"}
	colorAccent = lipgloss.AdaptiveColor{Dark: "#7aa2f7", Light: "#3d6fcc"}
	colorDanger = lipgloss.AdaptiveColor{Dark: "#f7768e", Light: "#c24242"}
	colorOk     = lipgloss.AdaptiveColor{Dark: "#9ece6a", Light: "#4e8a3a"}
	colorWarn   = lipgloss.AdaptiveColor{Dark: "#e0af68", Light: "#b36d00"}
)

// Styles holds all lipgloss styles used by the TUI.
var (
	// frameStyle is the outer rounded border that wraps the whole TUI,
	// forming a single application window. Width/Height are set at render
	// time in View() so the frame always fills the terminal exactly.
	frameStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim).
			Padding(0, 0)

	// tabStripStyle is the container for the top tab strip row. It carries
	// a muted bottom border that visually separates the tab strip from the
	// content area. Width is set at render time to the inner content width.
	tabStripStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(colorDim).
			Padding(0, 1)

	// paneTitleStyle is the small in-pane heading rendered inside a column
	// (e.g. "Groups (n)" or a list header) — not the tab strip container.
	paneTitleStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1)

	// tabStyle is an inactive tab label: muted foreground, no background.
	tabStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(colorMuted)

	// activeTabStyle is the currently selected tab label: accent text with
	// bold weight and an underline. No background fill (restrained look).
	activeTabStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(colorAccent).
			Bold(true).
			Underline(true)

	// tabSeparatorStyle renders the space separator between tab labels.
	// Using space instead of │ for a calmer look.
	tabSeparatorStyle = lipgloss.NewStyle().
				Foreground(colorDim)

	// statusBarStyle is the bottom help/status bar.
	statusBarStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(colorMuted)

	// syncBadgeStyle renders the persistent sync status on the right of the
	// bottom bar (待推送条数 / 最近同步时间).
	syncBadgeStyle = lipgloss.NewStyle().
			Foreground(colorAccent)

	// errorBarStyle renders error messages at the bottom.
	errorBarStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(colorDanger)

	// warnBarStyle renders non-fatal warnings (e.g. skipped legacy configs)
	// at the bottom, visually distinct from errors.
	warnBarStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(colorWarn)

	// contentStyle wraps the main content area.
	contentStyle = lipgloss.NewStyle().Padding(0, 1)

	// paneStyle is a generic pane (left/right column) style.
	// Unfocused panes use minimal border (single line, dim color).
	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colorDim).
			Padding(0, 1)

	// activePaneStyle highlights the currently focused pane.
	// Focused pane uses accent-colored border.
	activePaneStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colorAccent).
			Padding(0, 1)

	// selectedLineStyle highlights the cursor line in a list.
	selectedLineStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAccent)

	// maskedValueStyle renders masked secrets: dim bullets, not warning color.
	maskedValueStyle = lipgloss.NewStyle().
				Foreground(colorDim)

	// searchOverlayStyle wraps the global search overlay.
	// Single calm border, no loud colors.
	searchOverlayStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorDim).
				Padding(1, 2)

	// typeBadgeStyles map a result type to a styled badge.
	// All badges use the same muted/accent family for consistency.
	typeBadgeStyles = map[string]lipgloss.Style{
		typeEnv:    lipgloss.NewStyle().Foreground(colorAccent),
		typeText:   lipgloss.NewStyle().Foreground(colorMuted),
		typeBackup: lipgloss.NewStyle().Foreground(colorMuted),
		typeConfig: lipgloss.NewStyle().Foreground(colorMuted),
		typeSSH:    lipgloss.NewStyle().Foreground(colorMuted),
		typeLLM:    lipgloss.NewStyle().Foreground(colorMuted),
		typeMCP:    lipgloss.NewStyle().Foreground(colorMuted),
	}

	// emptyStateStyle renders empty-state hints.
	emptyStateStyle = lipgloss.NewStyle().
			Italic(true).
			Foreground(colorMuted).
			Padding(1, 2)

	// faintTextStyle 灰显弱化信息（如零引用 KeyPair 的「未被引用」），
	// 与 emptyStateStyle/tabStyle 同一 muted 色系。
	faintTextStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	// successStyle renders success indicators (OK, checkmark replacements).
	successStyle = lipgloss.NewStyle().
			Foreground(colorOk)

	// accentStyle renders accent-colored text (for key hints in status bar).
	accentStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)
)

// mutedStyle returns a style with muted foreground color.
// Defined here so form.go and other files can use it.
func mutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorMuted)
}
