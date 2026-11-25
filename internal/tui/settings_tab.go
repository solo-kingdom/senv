package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// settingsTab 是外观设置页：提供主题切换与图标集（Nerd Font / Unicode / ASCII）
// 切换，并用信息卡展示同步状态（同步卡只在注入了 SyncSource 时出现）。
// 光标在 Theme 与 Icons 两个列表间连续移动：0..len(themes)-1 为主题，
// len(themes)..len(themes)+len(icons)-1 为图标集。
type settingsTab struct {
	mgr           Managers
	width, height int
	current       string
	currentIcons  string
	cursor        int
}

func newSettingsTab(mgr Managers) *settingsTab {
	t := &settingsTab{mgr: mgr}
	t.syncFromApplied()
	return t
}

// syncFromApplied 把列表选中项与 Current 对齐到实际已应用的主题和图标集。
// 进入/重入设置页时调用，避免构造时的默认值残留，或光标停在未应用的项上。
func (t *settingsTab) syncFromApplied() {
	id := currentThemeID
	if id == "" {
		id = NormalizeTheme(t.mgr.Theme)
	}
	t.current = id
	t.cursor = themeIndex(id)

	iconID := currentIconSetID
	if iconID == "" {
		iconID = NormalizeIconSet(t.mgr.Icons)
	}
	t.currentIcons = iconID
}

// appliedID 返回当前画面实际使用的主题；View 用它画 ● 与 Current，
// 不移动光标（j/k 浏览时仍停在用户所在行）。
func (t *settingsTab) appliedID() string {
	if currentThemeID != "" {
		t.current = currentThemeID
		return currentThemeID
	}
	if t.current != "" {
		return t.current
	}
	return NormalizeTheme(t.mgr.Theme)
}

func (t *settingsTab) appliedIconSetID() string {
	if currentIconSetID != "" {
		t.currentIcons = currentIconSetID
		return currentIconSetID
	}
	if t.currentIcons != "" {
		return t.currentIcons
	}
	return NormalizeIconSet(t.mgr.Icons)
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

func iconSetIndex(id string) int {
	id = NormalizeIconSet(id)
	for i, s := range allIconSets() {
		if s.ID == id {
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
		{[]string{"h", "l"}, "section", grpNav, false},
		{[]string{"enter", " "}, "apply", grpItem, false},
	}
}

func (t *settingsTab) InputMode() bool { return false }

func (t *settingsTab) Init() tea.Cmd {
	t.syncFromApplied()
	return nil
}
func (t *settingsTab) Reload() tea.Cmd { return nil }

func (t *settingsTab) SetSize(width, height int) { t.width, t.height = width, height }

func (t *settingsTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return t, nil
	}
	themes := allThemes()
	icons := allIconSets()
	total := len(themes) + len(icons)
	if total == 0 {
		return t, nil
	}
	switch key.String() {
	case "j", "down":
		t.cursor = (t.cursor + 1) % total
	case "k", "up":
		t.cursor = (t.cursor - 1 + total) % total
	case "l", "right":
		if t.cursor < len(themes) && len(icons) > 0 {
			t.cursor = len(themes) + iconSetIndex(t.appliedIconSetID())
		}
	case "h", "left":
		if t.cursor >= len(themes) && len(themes) > 0 {
			t.cursor = themeIndex(t.appliedID())
		}
	case "enter", " ":
		return t, t.apply()
	}
	return t, nil
}

func (t *settingsTab) apply() tea.Cmd {
	themes := allThemes()
	icons := allIconSets()
	if t.cursor >= 0 && t.cursor < len(themes) {
		id := applyTheme(themes[t.cursor].ID)
		t.current = id
		if t.mgr.SaveTheme != nil {
			if err := t.mgr.SaveTheme(id); err != nil {
				return func() tea.Msg { return errMsg{err: err} }
			}
		}
		return okToast("theme: " + ThemeName(id))
	}
	idx := t.cursor - len(themes)
	if idx >= 0 && idx < len(icons) {
		id := applyIconSet(icons[idx].ID)
		t.currentIcons = id
		if t.mgr.SaveIcons != nil {
			if err := t.mgr.SaveIcons(id); err != nil {
				return func() tea.Msg { return errMsg{err: err} }
			}
		}
		return okToast("icons: " + IconSetName(id))
	}
	return nil
}

func (t *settingsTab) View() string {
	w := maxInt(t.width, 8)
	themes := allThemes()
	icons := allIconSets()

	sideBySide := w >= 74
	var cardW int
	if sideBySide {
		cardW = minInt((w-1)/2, 48)
	} else {
		cardW = minInt(maxInt(w-2, 24), 56)
		if cardW > w {
			cardW = w
		}
	}
	contentW := maxInt(cardW-4, 8)

	// Theme 卡片
	var themeLines []string
	current := t.appliedID()
	themeLines = append(themeLines, formatCardLine(
		padLabel("Current", 7),
		ThemeName(current),
		cardCyan,
		contentW,
	))
	themeLines = append(themeLines, "")
	for i, th := range themes {
		mark := "○"
		if th.ID == current {
			mark = "●"
		}
		line := mark + "  " + th.Name
		themeLines = append(themeLines, cursorLineW(line, i == t.cursor, contentW))
	}
	themeFocused := t.cursor < len(themes)
	themeCard := boxWithTitle("Theme", strings.Join(themeLines, "\n"), cardW-2, -1, themeFocused)

	// Icons 卡片
	var iconLines []string
	curIcons := t.appliedIconSetID()
	iconLines = append(iconLines, formatCardLine(
		padLabel("Current", 7),
		IconSetName(curIcons),
		cardCyan,
		contentW,
	))
	iconLines = append(iconLines, formatCardLine(
		padLabel("Preview", 7),
		iconPreviewSample(),
		cardPlain,
		contentW,
	))
	iconLines = append(iconLines, "")
	for i, ic := range icons {
		mark := "○"
		if ic.ID == curIcons {
			mark = "●"
		}
		line := mark + "  " + ic.Name
		iconLines = append(iconLines, cursorLineW(line, len(themes)+i == t.cursor, contentW))
	}
	iconFocused := t.cursor >= len(themes)
	iconCard := boxWithTitle("Icons", strings.Join(iconLines, "\n"), cardW-2, -1, iconFocused)

	var blocks []string
	if sideBySide {
		blocks = append(blocks, lipgloss.JoinHorizontal(lipgloss.Top, themeCard, " ", iconCard))
	} else {
		blocks = append(blocks, themeCard, "", iconCard)
	}

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
		syncW := cardW
		if sideBySide {
			syncW = cardW*2 + 1
		}
		blocks = append(blocks, "", infoCard("Sync", []cardRow{
			{Label: "Status", Value: status, Kind: kind},
			{Label: "Last sync", Value: last, Kind: cardCyan},
		}, syncW))
	}
	return strings.Join(blocks, "\n")
}

func padLabel(s string, width int) string {
	if pad := width - lipgloss.Width(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}
