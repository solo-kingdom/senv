package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// settingsTab 是外观设置页：当前先提供主题切换，并用信息卡展示主题与
// 同步状态（同步卡只在注入了 SyncSource 时出现）。
type settingsTab struct {
	mgr           Managers
	width, height int
	current       string
	cursor        int
}

func newSettingsTab(mgr Managers) *settingsTab {
	id := currentThemeID
	if id == "" {
		id = NormalizeTheme(mgr.Theme)
	}
	t := &settingsTab{mgr: mgr, current: id}
	t.cursor = themeIndex(id)
	return t
}

func themeIndex(id string) int {
	id = NormalizeTheme(id)
	for i, th := range allThemes() {
		if th.ID == id {
			return i
		}
	}
	return 0
}

func (t *settingsTab) Title() string { return "Settings" }

func (t *settingsTab) Bindings() []KeyAction {
	return []KeyAction{
		{[]string{"j", "down"}, "next", grpNav, false},
		{[]string{"k", "up"}, "prev", grpNav, false},
		{[]string{"enter", " "}, "apply", grpItem, false},
	}
}

func (t *settingsTab) InputMode() bool { return false }

func (t *settingsTab) Init() tea.Cmd   { return nil }
func (t *settingsTab) Reload() tea.Cmd { return nil }

func (t *settingsTab) SetSize(width, height int) { t.width, t.height = width, height }

func (t *settingsTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return t, nil
	}
	themes := allThemes()
	n := len(themes)
	if n == 0 {
		return t, nil
	}
	switch key.String() {
	case "j", "down":
		t.cursor = (t.cursor + 1) % n
	case "k", "up":
		t.cursor = (t.cursor - 1 + n) % n
	case "enter", " ":
		return t, t.apply()
	}
	return t, nil
}

func (t *settingsTab) apply() tea.Cmd {
	themes := allThemes()
	if t.cursor < 0 || t.cursor >= len(themes) {
		return nil
	}
	id := applyTheme(themes[t.cursor].ID)
	t.current = id
	if t.mgr.SaveTheme != nil {
		if err := t.mgr.SaveTheme(id); err != nil {
			return func() tea.Msg { return errMsg{err: err} }
		}
	}
	return okToast("theme: " + ThemeName(id))
}

func (t *settingsTab) View() string {
	w := maxInt(t.width, 8)
	cardW := minInt(maxInt(w-2, 24), 56)
	if cardW > w {
		cardW = w
	}
	contentW := maxInt(cardW-4, 8)

	var themeLines []string
	themeLines = append(themeLines, formatCardLine(
		padLabel("Current", 7),
		ThemeName(t.current),
		cardCyan,
		contentW,
	))
	themeLines = append(themeLines, "")
	for i, th := range allThemes() {
		mark := "○"
		if th.ID == t.current {
			mark = "●"
		}
		line := mark + "  " + th.Name
		themeLines = append(themeLines, cursorLineW(line, i == t.cursor, contentW))
	}
	blocks := []string{infoCardBlock("Theme", strings.Join(themeLines, "\n"), cardW)}

	if t.mgr.Sync != nil {
		st := t.mgr.Sync.Status()
		status, kind := "OK", cardOK
		switch {
		case st.Err != nil:
			status, kind = shortReason(st.Err), cardDanger
		case st.Dirty > 0:
			status, kind = fmt.Sprintf("pending %d", st.Dirty), cardWarn
		}
		last := "—"
		if !st.Last.IsZero() {
			last = st.Last.Local().Format("2006/01/02 15:04")
		}
		blocks = append(blocks, "", infoCard("Sync", []cardRow{
			{Label: "Status", Value: status, Kind: kind},
			{Label: "Last sync", Value: last, Kind: cardCyan},
		}, cardW))
	}
	return strings.Join(blocks, "\n")
}

func padLabel(s string, width int) string {
	if pad := width - lipgloss.Width(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}
