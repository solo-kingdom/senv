package tui

import (
	"math"
	"strconv"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// 常见终端背景：深色主题（纯黑、Tokyo Night、One Dark、Nord）与浅色主题。
var (
	darkBackgrounds  = []string{"#000000", "#1a1b26", "#282c34", "#2e3440"}
	lightBackgrounds = []string{"#ffffff", "#f5f5f5", "#fdf6e3"}
)

func relLuminance(t *testing.T, hex string) float64 {
	t.Helper()
	if len(hex) != 7 || hex[0] != '#' {
		t.Fatalf("bad hex %q", hex)
	}
	var c [3]float64
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		if err != nil {
			t.Fatalf("bad hex %q", hex)
		}
		x := float64(v) / 255
		if x <= 0.03928 {
			c[i] = x / 12.92
		} else {
			c[i] = math.Pow((x+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
}

func contrastRatio(t *testing.T, a, b string) float64 {
	la, lb := relLuminance(t, a), relLuminance(t, b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// TestPaletteContrast 守住每套主题的可读性下限（深色主题下灰蓝
// 文本过暗、看不清是用户反馈的问题）：正文/次要文本/强调色 ≥ 4.5:1
// （WCAG AA），边框/分隔线 ≥ 3:1；次要文本与正文仍可区分。
func TestPaletteContrast(t *testing.T) {
	t.Cleanup(func() { applyTheme(ThemeTokyoNight) })
	textRoles := []string{"fg", "muted", "accent", "cyan", "magenta", "danger", "ok", "warn"}
	for _, th := range allThemes() {
		t.Run(th.ID, func(t *testing.T) {
			applyTheme(th.ID)
			colors := map[string]lipgloss.TerminalColor{
				"fg": colorFg, "muted": colorMuted, "accent": colorAccent, "cyan": colorCyan,
				"magenta": colorMagenta, "danger": colorDanger, "ok": colorOk, "warn": colorWarn,
			}
			modes := []struct {
				name string
				dark bool
				bgs  []string
			}{
				{"dark", true, darkBackgrounds},
				{"light", false, lightBackgrounds},
			}
			switch th.ID {
			case ThemeGruvboxDark:
				modes = modes[:1]
			case ThemeGruvboxLight:
				modes = modes[1:]
			}
			for _, mode := range modes {
				pick := func(c lipgloss.TerminalColor) string { return colorHex(t, c, mode.dark) }
				bgs := append(append([]string{}, mode.bgs...), pick(colorSurface), pick(colorSurfaceHi))
				for _, name := range textRoles {
					c := colors[name]
					for _, bg := range bgs {
						if r := contrastRatio(t, pick(c), bg); r < 4.5 {
							t.Errorf("%s %s %s on %s: contrast %.2f < 4.5", mode.name, name, pick(c), bg, r)
						}
					}
				}
				for _, bg := range mode.bgs {
					if r := contrastRatio(t, pick(colorDim), bg); r < 3 {
						t.Errorf("%s dim %s on %s: contrast %.2f < 3", mode.name, pick(colorDim), bg, r)
					}
				}
				if r := contrastRatio(t, pick(colorFg), pick(colorMuted)); r < 1.3 {
					t.Errorf("%s muted too close to fg (%.2f): secondary text must stay distinguishable", mode.name, r)
				}
				if r := contrastRatio(t, pick(colorOnAccent), pick(colorAccent)); r < 4.5 {
					t.Errorf("%s active tab pill contrast %.2f < 4.5", mode.name, r)
				}
			}
		})
	}
}

// TestReadableRolesNeverUseDim 防止回退：标签、面板标题、掩码值这些需要
// 阅读的文本不得再用边框色 colorDim。
func TestReadableRolesNeverUseDim(t *testing.T) {
	for name, st := range map[string]lipgloss.Style{
		"tab": tabStyle, "paneTitle": paneTitleStyle, "statusBar": statusBarStyle,
		"masked": maskedValueStyle, "empty": emptyStateStyle, "config badge": typeBadgeStyles[typeConfig],
		"unselected": unselectedLineStyle,
	} {
		if st.GetForeground() == lipgloss.TerminalColor(colorDim) {
			t.Errorf("%s uses colorDim for text", name)
		}
	}
}

// colorHex 取出一个调色板角色的十六进制值。AdaptiveColor 按终端深浅取
// Dark/Light；显式主题的 lipgloss.Color 只有一个值，两种模式都返回它。
func colorHex(t *testing.T, c lipgloss.TerminalColor, dark bool) string {
	t.Helper()
	switch v := c.(type) {
	case lipgloss.AdaptiveColor:
		if dark {
			return v.Dark
		}
		return v.Light
	case lipgloss.Color:
		return string(v)
	default:
		t.Fatalf("unsupported color type %T", c)
		return ""
	}
}
