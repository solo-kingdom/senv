package tui

import "github.com/charmbracelet/lipgloss"

// Color palette for the TUI (Tokyo Night based by default). applyTheme
// swaps the whole set for a named theme. Tokyo Night keeps AdaptiveColor
// so lipgloss can pick Dark or Light from the terminal; explicit themes
// use lipgloss.Color so a light theme stays light on a dark terminal.
//
// Contrast floor (enforced by TestPaletteContrast against common dark and
// light terminal backgrounds and our own surface fills): every text role is
// >= 4.5:1 (WCAG AA), muted/secondary text stays clearly legible but
// visibly quieter than primary text, and colorDim — reserved for borders and
// separators, never for text you need to read — is >= 3:1.
var (
	colorFg        lipgloss.TerminalColor
	colorMuted     lipgloss.TerminalColor
	colorDim       lipgloss.TerminalColor
	colorAccent    lipgloss.TerminalColor
	colorCyan      lipgloss.TerminalColor
	colorMagenta   lipgloss.TerminalColor
	colorDanger    lipgloss.TerminalColor
	colorOk        lipgloss.TerminalColor
	colorWarn      lipgloss.TerminalColor
	colorSurface   lipgloss.TerminalColor
	colorSurfaceHi lipgloss.TerminalColor
	colorOnAccent  lipgloss.TerminalColor
)

// Styles holds all lipgloss styles used by the TUI. They are rebuilt by
// applyTheme so a runtime theme switch takes effect on the next View.
var (
	tabStripStyle       lipgloss.Style
	topBarStyle         lipgloss.Style
	brandStyle          lipgloss.Style
	agentPillStyle      lipgloss.Style
	agentPillOnStyle    lipgloss.Style
	providerBadgeStyle  lipgloss.Style
	chipKeyStyle        lipgloss.Style
	chipDescStyle       lipgloss.Style
	hintGroupStyle      lipgloss.Style
	paneTitleStyle      lipgloss.Style
	tabStyle            lipgloss.Style
	activeTabStyle      lipgloss.Style
	tabSeparatorStyle   lipgloss.Style
	statusBarStyle      lipgloss.Style
	syncBadgeStyle      lipgloss.Style
	errorBarStyle       lipgloss.Style
	warnBarStyle        lipgloss.Style
	contentStyle        lipgloss.Style
	paneStyle           lipgloss.Style
	activePaneStyle     lipgloss.Style
	selectedLineStyle   lipgloss.Style
	unselectedLineStyle lipgloss.Style
	maskedValueStyle    lipgloss.Style
	searchOverlayStyle  lipgloss.Style
	typeBadgeStyles     map[string]lipgloss.Style
	emptyStateStyle     lipgloss.Style
	faintTextStyle      lipgloss.Style
	successStyle        lipgloss.Style
	accentStyle         lipgloss.Style
	timestampStyle      lipgloss.Style
	dangerStyle         lipgloss.Style
)

func rebuildStyles() {
	tabStripStyle = lipgloss.NewStyle().
		Background(colorSurface).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(colorDim).
		Padding(0, 1)

	// topBarStyle 与 tabStripStyle 同形：一行内容 + 底边下划线，高度 2。
	topBarStyle = tabStripStyle

	brandStyle = lipgloss.NewStyle().
		Foreground(colorAccent).
		Background(colorSurface).
		Bold(true)

	agentPillStyle = lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(colorMuted).
		Background(colorSurface)

	agentPillOnStyle = lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(colorOnAccent).
		Background(colorCyan).
		Bold(true)

	providerBadgeStyle = lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(colorOnAccent).
		Background(colorAccent).
		Bold(true)

	chipKeyStyle = lipgloss.NewStyle().
		Foreground(colorOnAccent).
		Background(colorAccent).
		Bold(true).
		Padding(0, 1)

	chipDescStyle = lipgloss.NewStyle().
		Foreground(colorMuted)

	hintGroupStyle = lipgloss.NewStyle().
		Foreground(colorAccent).
		Bold(true)

	// paneTitleStyle is the small in-pane heading rendered inside a column
	// (e.g. "Groups (n)" or a list header) — not the tab strip container.
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
		Foreground(colorDim).
		Background(colorSurface)

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
		Foreground(colorOnAccent).
		Background(colorCyan)

	unselectedLineStyle = lipgloss.NewStyle().
		Foreground(colorFg).
		Background(colorSurface)

	maskedValueStyle = lipgloss.NewStyle().
		Foreground(colorMuted)

	searchOverlayStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
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
}

func mutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorMuted)
}

// frameBorderStyle colors the outer frame drawn by frameLayout.renderFrame.
func frameBorderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorDim).Background(colorSurface)
}
