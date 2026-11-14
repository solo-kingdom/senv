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
	colorDim     = lipgloss.AdaptiveColor{Dark: "#8b93b8", Light: "#7d8296"}
	colorAccent  = lipgloss.AdaptiveColor{Dark: "#7aa2f7", Light: "#2f5fbf"}
	colorCyan    = lipgloss.AdaptiveColor{Dark: "#7dcfff", Light: "#0b6a8a"}
	colorMagenta = lipgloss.AdaptiveColor{Dark: "#bb9af7", Light: "#7a4db8"}
	colorDanger  = lipgloss.AdaptiveColor{Dark: "#f7768e", Light: "#a83232"}
	colorOk      = lipgloss.AdaptiveColor{Dark: "#9ece6a", Light: "#356a26"}
	colorWarn    = lipgloss.AdaptiveColor{Dark: "#e0af68", Light: "#925800"}

	// Surfaces follow Tokyo Night Storm: lifted off the near-black Night
	// background so chrome (top bar, nav rail, footer) does not feel cave-like.
	colorSurface   = lipgloss.AdaptiveColor{Dark: "#24283b", Light: "#eceef3"}
	colorSurfaceHi = lipgloss.AdaptiveColor{Dark: "#292e42", Light: "#e1e5ef"}
	colorOnAccent  = lipgloss.AdaptiveColor{Dark: "#1a1b26", Light: "#ffffff"}
)

// Styles holds all lipgloss styles used by the TUI.
var (
	tabStripStyle = lipgloss.NewStyle().
			Background(colorSurface).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(colorDim).
			Padding(0, 1)

	// topBarStyle 与历史 tabStripStyle 同形：一行内容 + 底边下划线，高度 2。
	// 保留 tabStripStyle 别名以免既有测试与引用一起改名。
	topBarStyle = tabStripStyle

	navPaneStyle = lipgloss.NewStyle().
			Background(colorSurface)

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
				Foreground(colorOnAccent).
				Background(colorCyan)

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
)

func mutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorMuted)
}

// frameBorderStyle colors the outer frame drawn by frameLayout.renderFrame.
func frameBorderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorDim)
}
