package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestApplyPaneLiftsTitleIntoBorder(t *testing.T) {
	withTrueColor(t)
	applyTheme(ThemeGruvboxLight)

	body := windowedPane("Groups (3)", []string{"  alpha", "  beta"}, 0, 4, 20)
	out := applyPane(body, 20, 4, true)
	plain := ansi.Strip(out)
	lines := strings.Split(plain, "\n")
	if len(lines) == 0 || !strings.Contains(lines[0], "Groups (3)") {
		t.Fatalf("title not in top border: %q", plain)
	}
	if strings.Contains(lines[0], "alpha") {
		t.Fatalf("body leaked into title: %q", lines[0])
	}
	if !strings.Contains(plain, "alpha") || !strings.Contains(plain, "beta") {
		t.Fatalf("body missing after lift: %q", plain)
	}
	if w := lipgloss.Width(out); w != 22 {
		t.Fatalf("width=%d want 22\n%s", w, plain)
	}
	if h := lipgloss.Height(out); h != 6 {
		t.Fatalf("height=%d want 6\n%s", h, plain)
	}
	// 焦点面板边框用强调色 #076678。
	if !strings.Contains(out, "38;2;7;102;120") {
		t.Fatalf("active pane missing accent border: %q", out)
	}
}

func TestApplyPaneLeavesEmptyStateUntitled(t *testing.T) {
	withTrueColor(t)
	applyTheme(ThemeGruvboxLight)

	body := emptyStateStyle.Render("loading groups…")
	out := applyPane(body, 20, 4, false)
	plain := ansi.Strip(out)
	lines := strings.Split(plain, "\n")
	if strings.Contains(lines[0], "loading") {
		t.Fatalf("empty state should not be lifted into the title: %q", lines[0])
	}
	if !strings.Contains(plain, "loading groups") {
		t.Fatalf("empty state body missing: %q", plain)
	}
}

func TestRenderTabIconsUseDistinctColors(t *testing.T) {
	prev := lipgloss.DefaultRenderer().ColorProfile()
	lipgloss.DefaultRenderer().SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.DefaultRenderer().SetColorProfile(prev) })
	applyTheme(ThemeGruvboxLight)
	t.Cleanup(func() { applyTheme(ThemeTokyoNight) })

	env := renderTab("Env", false)
	text := renderTab("Text", false)
	if env == text {
		t.Fatalf("Env and Text icons should use different colors")
	}
	// Env 图标用 cyan #276749，Text 用 accent #076678。
	if !strings.Contains(env, "38;2;39;103;73") {
		t.Fatalf("Env icon missing cyan fg: %q", env)
	}
	if !strings.Contains(text, "38;2;7;102;120") {
		t.Fatalf("Text icon missing accent fg: %q", text)
	}
	active := renderTab("Env", true)
	// 激活标题胶囊底色是 accent #076678，图标仍在 surface 上。
	if !strings.Contains(active, "48;2;7;102;120") {
		t.Fatalf("active title missing accent pill: %q", active)
	}
	if lipgloss.Height(active) != 1 {
		t.Fatalf("renderTab height=%d want 1", lipgloss.Height(active))
	}
}
