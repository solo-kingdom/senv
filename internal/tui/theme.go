package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// 内置主题 id。东京夜是默认（跟随终端深/浅），另外两套是可切换的
// Gruvbox 明/暗。颜色值在 WCAG AA 下限上相对官方 Gruvbox 做了少量抬升
// （见 palette_contrast_test.go）。
const (
	ThemeTokyoNight   = "tokyo-night"
	ThemeGruvboxDark  = "gruvbox-dark"
	ThemeGruvboxLight = "gruvbox-light"
)

// ThemeInfo 是设置页列出的一项主题。
type ThemeInfo struct {
	ID, Name string
}

var builtinThemes = []ThemeInfo{
	{ID: ThemeTokyoNight, Name: "Tokyo Night"},
	{ID: ThemeGruvboxDark, Name: "Gruvbox Dark"},
	{ID: ThemeGruvboxLight, Name: "Gruvbox Light"},
}

// currentThemeID 是当前已应用到包级样式的主题；空表示尚未 apply。
var currentThemeID string

// NormalizeTheme 把用户/配置里的主题名收敛到内置 id；无法识别时回落到
// 东京夜。接受连字符与空格两种写法（"gruvbox dark" / "gruvbox-dark"）。
func NormalizeTheme(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.Join(strings.Fields(s), "-")
	switch s {
	case ThemeGruvboxDark, "gruvbox":
		return ThemeGruvboxDark
	case ThemeGruvboxLight:
		return ThemeGruvboxLight
	default:
		return ThemeTokyoNight
	}
}

// ThemeName 返回主题的展示名。
func ThemeName(id string) string {
	id = NormalizeTheme(id)
	for _, t := range builtinThemes {
		if t.ID == id {
			return t.Name
		}
	}
	return builtinThemes[0].Name
}

func allThemes() []ThemeInfo { return builtinThemes }

type palette struct {
	fg, muted, dim, accent, cyan, magenta, danger, ok, warn string
	surface, surfaceHi, onAccent                            string
}

func staticColor(hex string) lipgloss.TerminalColor {
	// 显式主题必须用 Color 而不是 AdaptiveColor{Light:hex, Dark:hex}：
	// 后者语义上也能固定颜色，但强制主题应当不依赖终端深浅检测。
	return lipgloss.Color(hex)
}

// tokyoNight 保持 AdaptiveColor：同一套角色在深/浅终端下各取一值。
func applyTokyoNight() {
	colorFg = lipgloss.AdaptiveColor{Dark: "#c0caf5", Light: "#343b58"}
	colorMuted = lipgloss.AdaptiveColor{Dark: "#9aa5ce", Light: "#565a6e"}
	colorDim = lipgloss.AdaptiveColor{Dark: "#8b93b8", Light: "#7d8296"}
	colorAccent = lipgloss.AdaptiveColor{Dark: "#7aa2f7", Light: "#2f5fbf"}
	colorCyan = lipgloss.AdaptiveColor{Dark: "#7dcfff", Light: "#0b6a8a"}
	colorMagenta = lipgloss.AdaptiveColor{Dark: "#bb9af7", Light: "#7a4db8"}
	colorDanger = lipgloss.AdaptiveColor{Dark: "#f7768e", Light: "#a83232"}
	colorOk = lipgloss.AdaptiveColor{Dark: "#9ece6a", Light: "#356a26"}
	colorWarn = lipgloss.AdaptiveColor{Dark: "#e0af68", Light: "#925800"}
	colorSurface = lipgloss.AdaptiveColor{Dark: "#24283b", Light: "#eceef3"}
	colorSurfaceHi = lipgloss.AdaptiveColor{Dark: "#292e42", Light: "#e1e5ef"}
	colorOnAccent = lipgloss.AdaptiveColor{Dark: "#1a1b26", Light: "#ffffff"}
}

func applyPalette(p palette) {
	colorFg = staticColor(p.fg)
	colorMuted = staticColor(p.muted)
	colorDim = staticColor(p.dim)
	colorAccent = staticColor(p.accent)
	colorCyan = staticColor(p.cyan)
	colorMagenta = staticColor(p.magenta)
	colorDanger = staticColor(p.danger)
	colorOk = staticColor(p.ok)
	colorWarn = staticColor(p.warn)
	colorSurface = staticColor(p.surface)
	colorSurfaceHi = staticColor(p.surfaceHi)
	colorOnAccent = staticColor(p.onAccent)
}

// 官方 Gruvbox 的部分角色在 surface 上达不到 4.5:1（尤其是亮红、浅色
// 主题的 aqua / yellow / green）。这里只微调这些角色，其余保持原色。
var (
	paletteGruvboxDark = palette{
		fg: "#ebdbb2", muted: "#bdae93", dim: "#928374",
		accent: "#83a598", cyan: "#8ec07c", magenta: "#d3869b",
		danger: "#ff6f5c", ok: "#b8bb26", warn: "#fabd2f",
		surface: "#282828", surfaceHi: "#32302f", onAccent: "#1d2021",
	}
	paletteGruvboxLight = palette{
		fg: "#3c3836", muted: "#504945", dim: "#7c6f64",
		accent: "#076678", cyan: "#276749", magenta: "#8f3f71",
		danger: "#9d0006", ok: "#5c580a", warn: "#7a4d0a",
		surface: "#fbf1c7", surfaceHi: "#ebdbb2", onAccent: "#fbf1c7",
	}
)

// applyTheme 把命名主题写进包级颜色与样式，返回收敛后的 id。
func applyTheme(id string) string {
	id = NormalizeTheme(id)
	switch id {
	case ThemeGruvboxDark:
		applyPalette(paletteGruvboxDark)
	case ThemeGruvboxLight:
		applyPalette(paletteGruvboxLight)
	default:
		applyTokyoNight()
		id = ThemeTokyoNight
	}
	rebuildStyles()
	currentThemeID = id
	return id
}

func init() { applyTheme(ThemeTokyoNight) }
