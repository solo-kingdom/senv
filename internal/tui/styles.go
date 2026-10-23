package tui

import "github.com/charmbracelet/lipgloss"

// Adaptive color palette for light/dark terminals (Tokyo Night based).
// lipgloss picks Dark or Light from the detected terminal background.
//
// Contrast floor (enforced by TestPaletteContrast against common dark and
// light terminal backgrounds and our own surface fills): every text role is
// >= 4.5:1 (WCAG AA), muted/secondary text stays clearly legible but
// visibly quieter than primary text, and colorDim — reserved for borders and
// separators, never for text you need to read — is >= 3:1.
var (
	// Core palette roles
	colorFg      = lipgloss.AdaptiveColor{Dark: "#c0caf5", Light: "#343b58"}
	colorMuted   = lipgloss.AdaptiveColor{Dark: "#9aa5ce", Light: "#565a6e"}
	colorDim     = lipgloss.AdaptiveColor{Dark: "#7f88b0", Light: "#7d8296"}
	colorAccent  = lipgloss.AdaptiveColor{Dark: "#7aa2f7", Light: "#2f5fbf"}
	colorCyan    = lipgloss.AdaptiveColor{Dark: "#7dcfff", Light: "#0b6a8a"}
	colorMagenta = lipgloss.AdaptiveColor{Dark: "#bb9af7", Light: "#7a4db8"}
	colorDanger  = lipgloss.AdaptiveColor{Dark: "#f7768e", Light: "#a83232"}
	colorOk      = lipgloss.AdaptiveColor{Dark: "#9ece6a", Light: "#356a26"}
	colorWarn    = lipgloss.AdaptiveColor{Dark: "#e0af68", Light: "#925800"}

	// Surfaces (subtle depth without loud fills)
	colorSurface   = lipgloss.AdaptiveColor{Dark: "#16161e", Light: "#eceef3"}
	colorSurfaceHi = lipgloss.AdaptiveColor{Dark: "#1f2335", Light: "#e1e5ef"}
	colorOnAccent  = lipgloss.AdaptiveColor{Dark: "#1a1b26", Light: "#ffffff"}
)

// Styles holds all lipgloss styles used by the TUI.
var (
	tabStripStyle = lipgloss.NewStyle().
			Background(colorSurface).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(colorDim).
			Padding(0, 1)

	// paneTitleStyle is the small in-pane heading rendered inside a column
	// (e.g. "Groups (n)" or a list header) — not the tab strip container.
	// Bold for better visual hierarchy against list rows.
	paneTitleStyle = lipgloss.NewStyle().
			Foreground(colorFg).
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
				Foreground(colorMuted)

	searchOverlayStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorDim).
				Padding(1, 2)

	typeBadgeStyles = map[string]lipgloss.Style{
		typeEnv:    lipgloss.NewStyle().Foreground(colorAccent),
		typeText:   lipgloss.NewStyle().Foreground(colorCyan),
		typeBackup: lipgloss.NewStyle().Foreground(colorMuted),
		typeConfig: lipgloss.NewStyle().Foreground(colorMuted),
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

// frameBorderStyle colors the outer frame drawn by frameLayout.renderFrame.
func frameBorderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorDim)
}
