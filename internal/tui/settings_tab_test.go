package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSettingsTabIsLast(t *testing.T) {
	m := New(Managers{})
	if last := m.tabs[len(m.tabs)-1].Title(); last != "Settings" {
		t.Fatalf("last tab = %q, want Settings", last)
	}
}

func TestSettingsThemeApplyAndPersist(t *testing.T) {
	t.Cleanup(func() { applyTheme(ThemeTokyoNight) })
	applyTheme(ThemeTokyoNight)

	var saved []string
	tab := newSettingsTab(Managers{
		SaveTheme: func(name string) error {
			saved = append(saved, name)
			return nil
		},
	})
	tab.SetSize(60, 20)

	view := ansi.Strip(tab.View())
	if !strings.Contains(view, "Theme") || !strings.Contains(view, "Tokyo Night") {
		t.Fatalf("theme card missing:\n%s", view)
	}
	if !strings.Contains(view, "Current") || !strings.Contains(view, " : ") {
		t.Fatalf("theme card should use label : value layout:\n%s", view)
	}
	if strings.Contains(view, "Sync") {
		t.Fatalf("sync card should be absent without SyncSource:\n%s", view)
	}

	// cursor starts on Tokyo Night; move to Gruvbox Dark and apply.
	darkIdx := themeIndex(ThemeGruvboxDark)
	for tab.cursor != darkIdx {
		out, _ := tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		tab = out.(*settingsTab)
	}
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = out.(*settingsTab)
	if cmd == nil {
		t.Fatal("apply should return a toast command")
	}
	if msg, ok := cmd().(toastMsg); !ok || !strings.Contains(msg.text, "Gruvbox Dark") {
		t.Fatalf("toast = %#v, want Gruvbox Dark", cmd())
	}
	if currentThemeID != ThemeGruvboxDark {
		t.Fatalf("current theme = %q, want %s", currentThemeID, ThemeGruvboxDark)
	}
	if tab.current != ThemeGruvboxDark {
		t.Fatalf("tab current = %q", tab.current)
	}
	if len(saved) != 1 || saved[0] != ThemeGruvboxDark {
		t.Fatalf("saved = %v, want [%s]", saved, ThemeGruvboxDark)
	}
	view = ansi.Strip(tab.View())
	if !strings.Contains(view, "Gruvbox Dark") {
		t.Fatalf("view after apply missing Gruvbox Dark:\n%s", view)
	}
}

func TestSettingsSyncCardUsesStatusStyle(t *testing.T) {
	t.Cleanup(func() { applyTheme(ThemeTokyoNight) })
	last := time.Date(2026, 2, 6, 22, 38, 0, 0, time.Local)
	tab := newSettingsTab(Managers{Sync: &fakeSyncSource{state: SyncState{Last: last}}})
	tab.SetSize(60, 20)
	view := ansi.Strip(tab.View())
	if !strings.Contains(view, "Sync") {
		t.Fatalf("sync card missing:\n%s", view)
	}
	if !strings.Contains(view, "Status") || !strings.Contains(view, "OK") {
		t.Fatalf("sync status missing:\n%s", view)
	}
	if !strings.Contains(view, "Last sync") || !strings.Contains(view, "2026/02/06 22:38") {
		t.Fatalf("last sync missing or wrong format:\n%s", view)
	}
}

func TestInfoCardLabelValueLayout(t *testing.T) {
	card := ansi.Strip(infoCard("WebDAV Sync", []cardRow{
		{Label: "Status", Value: "OK", Kind: cardOK},
		{Label: "Last sync", Value: "2026/02/06 22:38", Kind: cardCyan},
	}, 40))
	lines := strings.Split(card, "\n")
	if len(lines) < 4 {
		t.Fatalf("card too short:\n%s", card)
	}
	if !strings.HasPrefix(lines[0], "╭") || !strings.Contains(lines[0], "WebDAV Sync") {
		t.Fatalf("title not in top border: %q", lines[0])
	}
	if !strings.HasPrefix(lines[len(lines)-1], "╰") {
		t.Fatalf("missing bottom border: %q", lines[len(lines)-1])
	}
	if !strings.Contains(lines[1], "Status") || !strings.Contains(lines[1], " : ") || !strings.Contains(lines[1], "OK") {
		t.Fatalf("status row: %q", lines[1])
	}
	if !strings.Contains(lines[2], "Last sync") || !strings.Contains(lines[2], "2026/02/06 22:38") {
		t.Fatalf("last sync row: %q", lines[2])
	}
	// labels share a colon column
	c1 := strings.Index(lines[1], " : ")
	c2 := strings.Index(lines[2], " : ")
	if c1 < 0 || c1 != c2 {
		t.Fatalf("colon columns not aligned: %d vs %d\n%s", c1, c2, card)
	}
}

func TestTabIconsHaveStableWidth(t *testing.T) {
	for _, title := range []string{"Env", "Text", "Providers", "Config", "SSH", "KeyPair", "MCP", "History", "Audit", "Backup", "Settings"} {
		icon := tabIcon(title)
		got := sanitizeLine(icon)
		if got != icon {
			t.Errorf("icon for %s was sanitized %q -> %q", title, icon, got)
		}
		if gw, ww := ansi.StringWidth(icon), ansi.StringWidthWc(icon); gw != ww || gw != 1 {
			t.Errorf("icon for %s width grapheme=%d wcwidth=%d, want 1/1", title, gw, ww)
		}
	}
}

func TestNormalizeTheme(t *testing.T) {
	cases := map[string]string{
		"":              ThemeTokyoNight,
		"tokyo-night":   ThemeTokyoNight,
		"Gruvbox Dark":  ThemeGruvboxDark,
		"gruvbox_dark":  ThemeGruvboxDark,
		"gruvbox-light": ThemeGruvboxLight,
		"gruvbox light": ThemeGruvboxLight,
		"unknown":       ThemeTokyoNight,
	}
	for in, want := range cases {
		if got := NormalizeTheme(in); got != want {
			t.Errorf("NormalizeTheme(%q) = %q, want %q", in, got, want)
		}
	}
}

func settingsTabOf(t *testing.T, m Model) *settingsTab {
	t.Helper()
	for _, tab := range m.tabs {
		if s, ok := tab.(*settingsTab); ok {
			return s
		}
	}
	t.Fatal("settings tab missing")
	return nil
}

func TestSettingsTabSelectsPersistedThemeOnNew(t *testing.T) {
	t.Cleanup(func() { applyTheme(ThemeTokyoNight) })
	// 模拟包 init 已把 currentThemeID 设成默认主题，再以持久化的
	// Gruvbox Light 构造 Model：选中项必须跟实际应用的主题一致。
	applyTheme(ThemeTokyoNight)
	m := New(Managers{Theme: ThemeGruvboxLight})
	if currentThemeID != ThemeGruvboxLight {
		t.Fatalf("applied theme = %q, want %s", currentThemeID, ThemeGruvboxLight)
	}
	tab := settingsTabOf(t, m)
	if tab.current != ThemeGruvboxLight {
		t.Fatalf("tab current = %q, want %s", tab.current, ThemeGruvboxLight)
	}
	if tab.cursor != themeIndex(ThemeGruvboxLight) {
		t.Fatalf("cursor = %d, want %d (Gruvbox Light)", tab.cursor, themeIndex(ThemeGruvboxLight))
	}
	tab.SetSize(60, 20)
	view := ansi.Strip(tab.View())
	if !strings.Contains(view, "Current") || !strings.Contains(view, "Gruvbox Light") {
		t.Fatalf("current theme not echoed:\n%s", view)
	}
	if !strings.Contains(view, "●  Gruvbox Light") {
		t.Fatalf("Gruvbox Light should be marked selected:\n%s", view)
	}
	if strings.Contains(view, "●  Tokyo Night") {
		t.Fatalf("Tokyo Night must not stay selected after loading Gruvbox Light:\n%s", view)
	}
}

func TestSettingsTabReenterSelectsAppliedTheme(t *testing.T) {
	t.Cleanup(func() { applyTheme(ThemeTokyoNight) })
	applyTheme(ThemeTokyoNight)

	var saved []string
	tab := newSettingsTab(Managers{
		Theme: ThemeTokyoNight,
		SaveTheme: func(name string) error {
			saved = append(saved, name)
			return nil
		},
	})
	tab.SetSize(60, 20)

	lightIdx := themeIndex(ThemeGruvboxLight)
	for tab.cursor != lightIdx {
		out, _ := tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		tab = out.(*settingsTab)
	}
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = out.(*settingsTab)
	if cmd == nil {
		t.Fatal("apply should return a toast command")
	}
	if len(saved) != 1 || saved[0] != ThemeGruvboxLight {
		t.Fatalf("saved = %v, want [%s]", saved, ThemeGruvboxLight)
	}

	// 离开设置页前把光标移到未应用的项；重入时应回到已应用主题。
	out, _ = tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	tab = out.(*settingsTab)
	if tab.cursor == themeIndex(ThemeGruvboxLight) {
		t.Fatal("precondition: cursor should have moved off Gruvbox Light")
	}
	if c := tab.Init(); c != nil {
		t.Fatalf("Init should not emit a command, got %#v", c)
	}
	if tab.current != ThemeGruvboxLight {
		t.Fatalf("after re-enter current = %q, want %s", tab.current, ThemeGruvboxLight)
	}
	if tab.cursor != themeIndex(ThemeGruvboxLight) {
		t.Fatalf("after re-enter cursor = %d, want %d", tab.cursor, themeIndex(ThemeGruvboxLight))
	}
	view := ansi.Strip(tab.View())
	if !strings.Contains(view, "●  Gruvbox Light") {
		t.Fatalf("re-enter should mark Gruvbox Light selected:\n%s", view)
	}
}

func TestSettingsTabInitHealsStaleCursorAfterLateApply(t *testing.T) {
	t.Cleanup(func() { applyTheme(ThemeTokyoNight) })
	applyTheme(ThemeTokyoNight)
	tab := newSettingsTab(Managers{Theme: ThemeTokyoNight})
	if tab.current != ThemeTokyoNight {
		t.Fatalf("current = %q, want %s", tab.current, ThemeTokyoNight)
	}
	// 主题在 Tab 构造之后才应用（旧 New 顺序的回归）：Init 必须纠正选中项。
	applyTheme(ThemeGruvboxLight)
	tab.Init()
	if tab.current != ThemeGruvboxLight || tab.cursor != themeIndex(ThemeGruvboxLight) {
		t.Fatalf("after late apply+Init current=%q cursor=%d", tab.current, tab.cursor)
	}
}
