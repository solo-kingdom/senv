package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func withTrueColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.DefaultRenderer().ColorProfile()
	lipgloss.DefaultRenderer().SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() {
		lipgloss.DefaultRenderer().SetColorProfile(prev)
		applyTheme(ThemeTokyoNight)
	})
}

func TestFitLinePaintsSurfaceBackground(t *testing.T) {
	withTrueColor(t)
	applyTheme(ThemeGruvboxLight)

	got := fitLine("hello", 20)
	if w := ansi.StringWidth(got); w != 20 {
		t.Fatalf("width = %d, want 20: %q", w, got)
	}
	// #3c3836 on #fbf1c7 — 未设颜色的正文必须用主题前景，不能沿用终端默认浅色字。
	if !strings.Contains(got, "38;2;60;56;54") {
		t.Fatalf("unstyled text missing theme fg: %q", got)
	}
	if !strings.Contains(got, "48;2;251;241;199") {
		t.Fatalf("plain padding missing surface bg: %q", got)
	}

	pill := activeTabStyle.Render("Settings")
	got = fitLine(pill, 40)
	if w := ansi.StringWidth(got); w != 40 {
		t.Fatalf("pill row width = %d, want 40: %q", w, got)
	}
	// 活动胶囊的强调色底 (#076678) 不能被 surface 盖掉。
	if !strings.Contains(got, "48;2;7;102;120") {
		t.Fatalf("accent bg lost after paintSurface: %q", got)
	}
	if !strings.Contains(got, "48;2;251;241;199") {
		t.Fatalf("surface fill missing after accent pill: %q", got)
	}
}

func TestGruvboxLightFillsFrame(t *testing.T) {
	withTrueColor(t)
	applyTheme(ThemeGruvboxLight)

	l := computeLayout(80, 24)
	out := l.renderFrame("senv", "", "footer")
	lines := strings.Split(out, "\n")
	if len(lines) != 24 {
		t.Fatalf("rows = %d, want 24", len(lines))
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != 80 {
			t.Fatalf("row %d width %d, want 80: %q", i, w, line)
		}
		if !strings.Contains(line, "48;2;251;241;199") {
			t.Errorf("row %d missing light surface bg: %q", i, line)
		}
		if !strings.Contains(line, "38;2;60;56;54") {
			t.Errorf("row %d missing theme fg: %q", i, line)
		}
	}
}

func TestFitLineKeepsMutedForeground(t *testing.T) {
	withTrueColor(t)
	applyTheme(ThemeGruvboxLight)

	// 次要文本（#504945）不能被默认前景盖掉。
	got := fitLine(mutedStyle().Render("data-backup")+"  "+"plain", 40)
	if !strings.Contains(got, "38;2;80;73;69") {
		t.Fatalf("muted fg lost: %q", got)
	}
	if !strings.Contains(got, "38;2;60;56;54") {
		t.Fatalf("plain text after muted style missing theme fg: %q", got)
	}
}

func TestSGRResetsBackground(t *testing.T) {
	cases := []struct {
		seq  string
		want bool
	}{
		{"\x1b[0m", true},
		{"\x1b[m", true},
		{"\x1b[49m", true},
		{"\x1b[0;1;38;2;1;2;3m", true},
		{"\x1b[0;48;2;7;102;120m", false},
		{"\x1b[48;2;7;102;120m", false},
		{"\x1b[1;38;2;1;2;3m", false},
		{"\x1b[31m", false},
		{"\x1b[42m", false},
		{"hello", false},
	}
	for _, c := range cases {
		if got := sgrResetsBackground(c.seq); got != c.want {
			t.Errorf("sgrResetsBackground(%q) = %v, want %v", c.seq, got, c.want)
		}
	}
}

func TestUnselectedLineUsesThemeForeground(t *testing.T) {
	withTrueColor(t)
	applyTheme(ThemeGruvboxLight)

	line := cursorLineW("Tokyo Night", false, 20)
	if !strings.Contains(line, "38;2;60;56;54") {
		t.Fatalf("unselected line missing theme fg: %q", line)
	}
	if !strings.Contains(line, "48;2;251;241;199") {
		t.Fatalf("unselected line missing surface bg: %q", line)
	}

	card := infoCardBlock("Theme", cursorLineW("Tokyo Night", false, 24), 40)
	if !strings.Contains(card, "38;2;60;56;54") {
		t.Fatalf("theme card unselected text missing theme fg: %q", card)
	}
	plain := ansi.Strip(card)
	if !strings.Contains(plain, "╭ Theme ") {
		t.Fatalf("theme card title not in top border: %q", plain)
	}
}

func TestSGRSetsBackground(t *testing.T) {
	cases := []struct {
		seq  string
		want bool
	}{
		{"\x1b[48;2;7;102;120m", true},
		{"\x1b[42m", true},
		{"\x1b[100m", true},
		{"\x1b[0;48;2;7;102;120m", true},
		{"\x1b[0m", false},
		{"\x1b[49m", false},
		{"\x1b[38;2;60;56;54m", false},
		// Gruvbox Light colorDim #7c6f64：111 落在 100–107，不能被当成 ANSI 背景。
		{"\x1b[38;2;124;111;100m", false},
		// 256 色前景索引 40 也不是背景。
		{"\x1b[38;5;40m", false},
		{"\x1b[48;5;40m", true},
		{"hello", false},
	}
	for _, c := range cases {
		if got := sgrSetsBackground(c.seq); got != c.want {
			t.Errorf("sgrSetsBackground(%q) = %v, want %v", c.seq, got, c.want)
		}
	}
}

func TestFitLineFillsBorderAfterDimForeground(t *testing.T) {
	withTrueColor(t)
	applyTheme(ThemeGruvboxLight)

	// 标题复位后再接只带 dim 前景的分隔线：分隔线必须重新铺上 surface，
	// 不能露出终端默认黑底。
	got := fitLine(infoCardBlock("Theme", "  Tokyo Night", 56), 80)
	plain := ansi.Strip(got)
	if !strings.Contains(plain, "╭ Theme ") {
		t.Fatalf("missing title: %q", plain)
	}
	// 每条线（顶栏分隔线、正文、底栏、右框）在 0m 之后若还有边框字符，
	// 中间必须已经重新打开 surface 背景。
	for i, line := range strings.Split(got, "\n") {
		if !strings.Contains(line, "─") && !strings.Contains(line, "│") {
			continue
		}
		if !borderCharsHaveSurface(line) {
			t.Fatalf("line %d border missing surface bg: %q", i, line)
		}
	}
}

func borderCharsHaveSurface(s string) bool {
	surface := false
	i := 0
	for i < len(s) {
		if s[i] == 0x1b {
			end := strings.IndexByte(s[i:], 'm')
			if end < 0 {
				return false
			}
			seq := s[i : i+end+1]
			if sgrResetsBackground(seq) {
				surface = false
			}
			if sgrSetsBackground(seq) {
				surface = strings.Contains(seq, "48;2;251;241;199")
			}
			i += end + 1
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if strings.ContainsRune("╭╮╰╯─│", r) && !surface {
			return false
		}
		i += size
	}
	return true
}
