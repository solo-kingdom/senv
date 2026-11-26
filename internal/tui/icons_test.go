package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestAllIconSetsHaveStableWidthAndCompleteTabs(t *testing.T) {
	t.Cleanup(func() { applyIconSet(IconSetNerd) })
	allTabs := []string{
		"Env", "Text", "Providers", "Skill", "Config", "SSH", "KeyPair",
		"MCP", "Ledger", "Tree", "History", "Audit", "Backup", "Settings",
	}
	for _, set := range allIconSets() {
		t.Run(set.ID, func(t *testing.T) {
			applyIconSet(set.ID)
			pack := activeIconPack()
			for _, title := range allTabs {
				icon, ok := pack.tabs[title]
				if !ok || icon == "" {
					t.Fatalf("icon set %s missing dedicated icon for %s", set.ID, title)
				}
				if got := sanitizeLine(icon); got != icon {
					t.Errorf("%s/%s was sanitized %q -> %q", set.ID, title, icon, got)
				}
				if gw, ww := ansi.StringWidth(icon), ansi.StringWidthWc(icon); gw != ww || gw != 1 {
					t.Errorf("%s/%s width grapheme=%d wcwidth=%d, want 1/1", set.ID, title, gw, ww)
				}
			}
			for name, sym := range map[string]string{
				"cursor":    iconCursor(),
				"sync":      iconSync(),
				"search":    iconSearch(),
				"more":      iconMore(),
				"overflowL": iconOverflowL(),
				"overflowR": iconOverflowR(),
			} {
				if got := sanitizeLine(sym); got != sym {
					t.Errorf("%s/%s was sanitized %q -> %q", set.ID, name, sym, got)
				}
				if gw, ww := ansi.StringWidth(sym), ansi.StringWidthWc(sym); gw != ww || gw != 1 {
					t.Errorf("%s/%s width grapheme=%d wcwidth=%d, want 1/1", set.ID, name, gw, ww)
				}
			}
		})
	}
}

func TestPlainTextStillBlocksPUAWhenNerdFontEnabled(t *testing.T) {
	t.Cleanup(func() { applyIconSet(IconSetNerd) })
	applyIconSet(IconSetNerd)
	// 记录数据里混入即便是白名单内的 Nerd Font 码点，plainText 也必须折成 "?"。
	raw := "val " + tabIcon("Env") + " \ue0a0 ok"
	got := plainText(raw)
	if got != "val ? ? ok" {
		t.Fatalf("plainText(%q) = %q, want %q", raw, got, "val ? ? ok")
	}
}

func TestSettingsIconSetApplyAndPersist(t *testing.T) {
	t.Cleanup(func() {
		applyTheme(ThemeTokyoNight)
		applyIconSet(IconSetNerd)
	})
	applyTheme(ThemeTokyoNight)
	applyIconSet(IconSetNerd)

	var saved []string
	tab := newSettingsTab(Managers{
		Icons: IconSetNerd,
		SaveIcons: func(name string) error {
			saved = append(saved, name)
			return nil
		},
	})
	tab.SetSize(80, 24)

	view := ansi.Strip(tab.View())
	if !strings.Contains(view, "Icons") || !strings.Contains(view, "Nerd Font") || !strings.Contains(view, "Preview") {
		t.Fatalf("icons card missing:\n%s", view)
	}

	// 按 l 跳到 Icons 区域，按 j 选到 Unicode，按 enter 应用。
	out, _ := tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	tab = out.(*settingsTab)
	out, _ = tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	tab = out.(*settingsTab)
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = out.(*settingsTab)

	if cmd == nil {
		t.Fatal("apply icon set should return toast cmd")
	}
	if currentIconSetID != IconSetUnicode {
		t.Fatalf("currentIconSetID = %q, want %s", currentIconSetID, IconSetUnicode)
	}
	if len(saved) != 1 || saved[0] != IconSetUnicode {
		t.Fatalf("saved = %v, want [%s]", saved, IconSetUnicode)
	}
}

func TestTabStripThreeTierOverflow(t *testing.T) {
	t.Cleanup(func() { applyIconSet(IconSetNerd) })
	applyIconSet(IconSetNerd)
	titles := []string{
		"Env", "Text", "Providers", "Skill", "Config", "SSH", "KeyPair",
		"MCP", "Ledger", "Tree", "History", "Audit", "Backup", "Settings",
	}
	sep := tabSeparatorStyle.Render(" ")

	// 1. 超宽屏：完整模式，所有标题可见。
	wide := ansi.Strip(fitTabStripTitles(titles, 13, 220, sep))
	for _, title := range titles {
		if !strings.Contains(wide, title) {
			t.Fatalf("wide mode missing %s: %q", title, wide)
		}
	}

	// 2. 常见终端（预算 80 列）：紧凑模式，激活项显示完整标题，其余仅图标，14 个全可见。
	mid := ansi.Strip(fitTabStripTitles(titles, 13, 80, sep))
	if !strings.Contains(mid, "Settings") {
		t.Fatalf("compact mode missing active title Settings: %q", mid)
	}
	for _, title := range titles {
		if !strings.Contains(mid, tabIcon(title)) {
			t.Fatalf("compact mode missing icon for %s: %q", title, mid)
		}
	}

	// 3. 超窄屏（预算 32 列）：滑动视口，激活项完整，左右显示溢出指示。
	narrow := ansi.Strip(fitTabStripTitles(titles, 6, 32, sep))
	if !strings.Contains(narrow, "KeyPair") {
		t.Fatalf("sliding viewport missing active title KeyPair: %q", narrow)
	}
	if !strings.Contains(narrow, iconOverflowL()) || !strings.Contains(narrow, iconOverflowR()) {
		t.Fatalf("sliding viewport missing overflow arrows: %q", narrow)
	}
	if w := ansi.StringWidth(narrow); w > 32 {
		t.Fatalf("sliding viewport width %d > 32: %q", w, narrow)
	}
}

func TestApplyPaneDemotesInactiveSelection(t *testing.T) {
	prev := lipgloss.DefaultRenderer().ColorProfile()
	lipgloss.DefaultRenderer().SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() {
		lipgloss.DefaultRenderer().SetColorProfile(prev)
		applyTheme(ThemeTokyoNight)
	})
	applyTheme(ThemeGruvboxLight)

	row := selectedLineStyle.Render(cursorPrefix(true) + "alpha")
	activePane := applyPane(row, 20, 3, true)
	inactivePane := applyPane(row, 20, 3, false)

	// Gruvbox Light: accent=#076678 (7,102,120), surfaceHi=#ebdbb2 (235,219,178)
	if !strings.Contains(activePane, "48;2;7;102;120") {
		t.Fatalf("active pane should keep accent selection bg: %q", activePane)
	}
	if strings.Contains(inactivePane, "48;2;7;102;120") {
		t.Fatalf("inactive pane should not keep accent selection bg: %q", inactivePane)
	}
	if !strings.Contains(inactivePane, "48;2;235;219;178") {
		t.Fatalf("inactive pane should use surfaceHi selection bg: %q", inactivePane)
	}
}
