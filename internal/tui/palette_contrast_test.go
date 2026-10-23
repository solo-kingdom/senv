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

// TestPaletteContrast 守住深/浅两套主题下的可读性下限（深色主题下灰蓝
// 文本过暗、看不清是用户反馈的问题）：正文/次要文本/强调色 ≥ 4.5:1
// （WCAG AA），边框/分隔线 ≥ 3:1；次要文本与正文仍可区分。
func TestPaletteContrast(t *testing.T) {
	text := map[string]lipgloss.AdaptiveColor{
		"fg": colorFg, "muted": colorMuted, "accent": colorAccent, "cyan": colorCyan,
		"magenta": colorMagenta, "danger": colorDanger, "ok": colorOk, "warn": colorWarn,
	}
	modes := []struct {
		name string
		pick func(lipgloss.AdaptiveColor) string
		bgs  []string
	}{
		{"dark", func(c lipgloss.AdaptiveColor) string { return c.Dark }, darkBackgrounds},
		{"light", func(c lipgloss.AdaptiveColor) string { return c.Light }, lightBackgrounds},
	}
	for _, mode := range modes {
		// 文本也画在我们自己的底色上：tab 栏/状态栏（surface）与选中行（surfaceHi）。
		bgs := append(append([]string{}, mode.bgs...), mode.pick(colorSurface), mode.pick(colorSurfaceHi))
		for name, c := range text {
			for _, bg := range bgs {
				if r := contrastRatio(t, mode.pick(c), bg); r < 4.5 {
					t.Errorf("%s %s %s on %s: contrast %.2f < 4.5", mode.name, name, mode.pick(c), bg, r)
				}
			}
		}
		for _, bg := range mode.bgs {
			if r := contrastRatio(t, mode.pick(colorDim), bg); r < 3 {
				t.Errorf("%s dim %s on %s: contrast %.2f < 3", mode.name, mode.pick(colorDim), bg, r)
			}
		}
		if r := contrastRatio(t, mode.pick(colorFg), mode.pick(colorMuted)); r < 1.3 {
			t.Errorf("%s muted too close to fg (%.2f): secondary text must stay distinguishable", mode.name, r)
		}
		if r := contrastRatio(t, mode.pick(colorOnAccent), mode.pick(colorAccent)); r < 4.5 {
			t.Errorf("%s active tab pill contrast %.2f < 4.5", mode.name, r)
		}
	}
}

// TestReadableRolesNeverUseDim 防止回退：标签、面板标题、掩码值这些需要
// 阅读的文本不得再用边框色 colorDim。
func TestReadableRolesNeverUseDim(t *testing.T) {
	for name, st := range map[string]lipgloss.Style{
		"tab": tabStyle, "paneTitle": paneTitleStyle, "statusBar": statusBarStyle,
		"masked": maskedValueStyle, "empty": emptyStateStyle, "config badge": typeBadgeStyles[typeConfig],
	} {
		if st.GetForeground() == lipgloss.TerminalColor(colorDim) {
			t.Errorf("%s uses colorDim for text", name)
		}
	}
}
