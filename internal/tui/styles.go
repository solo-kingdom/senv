package tui

import "github.com/charmbracelet/lipgloss"

// Adaptive color palette for light/dark terminals (Tokyo Night).
// Using AdaptiveColor: first value is dark terminal, second is light terminal.
var (
	// Core palette roles
	colorFg      = lipgloss.AdaptiveColor{Dark: "#c0caf5", Light: "#343b58"}
	colorMuted   = lipgloss.AdaptiveColor{Dark: "#565f89", Light: "#9699a3"}
	colorDim     = lipgloss.AdaptiveColor{Dark: "#414868", Light: "#b4b5b9"}
	colorAccent  = lipgloss.AdaptiveColor{Dark: "#7aa2f7", Light: "#3d6fcc"}
	colorCyan    = lipgloss.AdaptiveColor{Dark: "#7dcfff", Light: "#0f7a9d"}
	colorMagenta = lipgloss.AdaptiveColor{Dark: "#bb9af7", Light: "#7a4db8"}
	colorDanger  = lipgloss.AdaptiveColor{Dark: "#f7768e", Light: "#c24242"}
	colorOk      = lipgloss.AdaptiveColor{Dark: "#9ece6a", Light: "#4e8a3a"}
	colorWarn    = lipgloss.AdaptiveColor{Dark: "#e0af68", Light: "#b36d00"}

	// Surfaces (subtle depth without loud fills)
	colorSurface     = lipgloss.AdaptiveColor{Dark: "#16161e", Light: "#e8eaf0"}
	colorSurfaceHi   = lipgloss.AdaptiveColor{Dark: "#1f2335", Light: "#d8dce8"}
	colorOnAccent    = lipgloss.AdaptiveColor{Dark: "#1a1b26", Light: "#ffffff"}
)

// Styles holds all lipgloss styles used by the TUI.
var (
	frameStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim).
			Padding(0, 0)

	tabStripStyle = lipgloss.NewStyle().
			Background(colorSurface).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(colorDim).
			Padding(0, 1)

	// paneTitleStyle is the small in-pane heading rendered inside a column
	// (e.g. "Groups (n)" or a list header) — not the tab strip container.
	// Bold for better visual hierarchy against list rows.
	paneTitleStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Bold(true).
			Padding(0, 1)

	tabStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(colorMuted)

	activeTabStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(colorOnAccent).
			Background(colorAccent).
			Bold(true)

	tabSeparatorStyle = lipgloss.NewStyle().
				Foreground(colorDim)

	statusBarStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(colorMuted).
			Background(colorSurface)

	syncBadgeStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Background(colorSurface)

	errorBarStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(colorDanger).
			Background(colorSurface)

	warnBarStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(colorWarn).
			Background(colorSurface)

	contentStyle = lipgloss.NewStyle().Padding(0, 1)

	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colorDim).
			Padding(0, 1)

	activePaneStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colorAccent).
			Padding(0, 1)

	selectedLineStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAccent).
				Background(colorSurfaceHi)

	maskedValueStyle = lipgloss.NewStyle().
				Foreground(colorDim)

	searchOverlayStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorDim).
				Padding(1, 2)

	typeBadgeStyles = map[string]lipgloss.Style{
		typeEnv:    lipgloss.NewStyle().Foreground(colorAccent),
		typeText:   lipgloss.NewStyle().Foreground(colorCyan),
		typeBackup: lipgloss.NewStyle().Foreground(colorMuted),
		typeConfig: lipgloss.NewStyle().Foreground(colorDim),
		typeSSH:    lipgloss.NewStyle().Foreground(colorMagenta),
		typeLLM:    lipgloss.NewStyle().Foreground(colorOk),
		typeMCP:    lipgloss.NewStyle().Foreground(colorWarn),
	}

	emptyStateStyle = lipgloss.NewStyle().
			Italic(true).
			Foreground(colorMuted).
			Padding(1, 2)

	faintTextStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	successStyle = lipgloss.NewStyle().
			Foreground(colorOk)

	accentStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	timestampStyle = lipgloss.NewStyle().
			Foreground(colorCyan)

	dangerStyle = lipgloss.NewStyle().
			Foreground(colorDanger).
			Bold(true)
)

func mutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorMuted)
}
