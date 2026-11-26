package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// welcomeJumpMsg asks the top-level model to jump from the Welcome page to a tab.
type welcomeJumpMsg struct{ index int }

type welcomeWorkspaceItem struct {
	index  int
	title  string
	desc   string
	hotkey string
}

// welcomeTab 是启动首屏（Home / 欢迎页）：展示当前环境摘要、全部工作区快速入口
// 与常用快捷键速查；支持 j/k/↑↓ 选择任意工作区按 Enter 直达，也可随时按 0 返回。
type welcomeTab struct {
	width, height int
	items         []welcomeWorkspaceItem
	cursor        int
	provider      string
	syncSummary   string
	syncKind      cardKind
}

func newWelcomeTab(tabs []Tab) *welcomeTab {
	items := make([]welcomeWorkspaceItem, 0, len(tabs))
	for i, t := range tabs {
		title := t.Title()
		hotkey := ""
		if i < 9 {
			hotkey = strconv.Itoa(i + 1)
		}
		if title == "Settings" {
			hotkey = ","
		}
		items = append(items, welcomeWorkspaceItem{
			index:  i,
			title:  title,
			desc:   tabDescription(title),
			hotkey: hotkey,
		})
	}
	return &welcomeTab{items: items}
}

func (w *welcomeTab) setStatus(provider, syncSummary string, syncKind cardKind) {
	w.provider = provider
	w.syncSummary = syncSummary
	w.syncKind = syncKind
}

func (w *welcomeTab) Title() string { return "Home" }

func (w *welcomeTab) Bindings() []KeyAction {
	return []KeyAction{
		{[]string{"j/k", "↑/↓"}, "select", grpNav, false},
		{[]string{"enter"}, "open workspace", grpItem, false},
		{[]string{"1–9"}, "quick tab", grpItem, false},
		{[]string{"ctrl+p"}, "tab switcher", grpItem, false},
		{[]string{"S"}, "global search", grpItem, false},
		{[]string{","}, "settings", grpItem, false},
	}
}

func (w *welcomeTab) InputMode() bool   { return false }
func (w *welcomeTab) Init() tea.Cmd     { return nil }
func (w *welcomeTab) Reload() tea.Cmd   { return nil }
func (w *welcomeTab) SetSize(W, H int)  { w.width, w.height = W, H }

func (w *welcomeTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return w, nil
	}
	n := len(w.items)
	if n == 0 {
		return w, nil
	}
	switch key.String() {
	case "j", "down":
		w.cursor = (w.cursor + 1) % n
	case "k", "up":
		w.cursor = (w.cursor - 1 + n) % n
	case "g", "home":
		w.cursor = 0
	case "G", "end":
		w.cursor = n - 1
	case "pgup":
		w.cursor = clamp(w.cursor-5, 0, n-1)
	case "pgdown":
		w.cursor = clamp(w.cursor+5, 0, n-1)
	case "enter":
		idx := w.items[clamp(w.cursor, 0, n-1)].index
		return w, func() tea.Msg { return welcomeJumpMsg{index: idx} }
	}
	return w, nil
}

func (w *welcomeTab) View() string {
	totalW := maxInt(w.width, 8)
	// Tab 约定的总槽位高是 height+2（见 layout.tabSize）。
	slotH := maxInt(w.height+2, 4)

	banner := w.renderBanner(totalW)
	bannerH := lipgloss.Height(banner)
	// 卡片区可用高度：总槽位高 - banner - 1 空行（空间紧张时省去空行）
	gap := 1
	if slotH-bannerH-1 < 5 {
		gap = 0
	}
	cardsH := maxInt(slotH-bannerH-gap, 3)

	var body string
	if totalW >= 76 {
		leftW := (totalW - 1) * 52 / 100
		if leftW < 34 {
			leftW = 34
		}
		rightW := totalW - leftW - 1
		leftCard := w.renderWorkspacesCard(leftW, cardsH)
		rightCard := w.renderShortcutsCard(rightW, cardsH)
		body = lipgloss.JoinHorizontal(lipgloss.Top, leftCard, " ", rightCard)
	} else {
		// 窄屏上下堆叠：优先把更多高度分给 Workspaces，Shortcuts 紧随其下。
		wsH := maxInt(cardsH*55/100, 3)
		scH := maxInt(cardsH-wsH, 3)
		leftCard := w.renderWorkspacesCard(totalW, wsH)
		rightCard := w.renderShortcutsCard(totalW, scH)
		body = lipgloss.JoinVertical(lipgloss.Left, leftCard, rightCard)
	}

	var parts []string
	parts = append(parts, banner)
	if gap > 0 {
		parts = append(parts, "")
	}
	parts = append(parts, body)
	return clipLines(lipgloss.JoinVertical(lipgloss.Left, parts...), slotH)
}

func (w *welcomeTab) renderBanner(width int) string {
	title := lipgloss.NewStyle().Foreground(colorAccent).Background(colorSurface).Bold(true).
		Render(tabIcon("Home") + " Welcome to Senv")
	tagline := lipgloss.NewStyle().Foreground(colorMuted).Background(colorSurface).
		Render("  Encrypted vault & agent workspace")

	var chips []string
	chips = append(chips,
		welcomeChip("Theme", ThemeName(currentThemeID), colorCyan),
		welcomeChip("Icons", IconSetName(currentIconSetID), colorMagenta),
	)
	if w.provider != "" {
		chips = append(chips, welcomeChip("Provider", w.provider, colorAccent))
	}
	if w.syncSummary != "" {
		sc := colorOk
		switch w.syncKind {
		case cardWarn:
			sc = colorWarn
		case cardDanger:
			sc = colorDanger
		}
		chips = append(chips, welcomeChip("Sync", w.syncSummary, sc))
	}

	line1 := truncateWidth(title+tagline, maxInt(width-4, 4))
	line2 := truncateWidth(strings.Join(chips, "  "), maxInt(width-4, 4))
	content := line1 + "\n" + line2
	return boxWithTitle("Overview", content, maxInt(width-2, 4), 2, false)
}

func welcomeChip(label, val string, valColor lipgloss.TerminalColor) string {
	l := lipgloss.NewStyle().Foreground(colorMuted).Background(colorSurface).Render(label + ": ")
	v := lipgloss.NewStyle().Foreground(valColor).Background(colorSurface).Bold(true).Render(val)
	return l + v
}

func (w *welcomeTab) renderWorkspacesCard(boxW, boxH int) string {
	innerW := maxInt(boxW-2, 4)
	contentW := maxInt(innerW-2, 1)
	innerH := maxInt(boxH-2, 1)

	start, end := visibleRange(len(w.items), w.cursor, innerH)
	title := fmt.Sprintf("Workspaces (%d)", len(w.items))
	if start > 0 || end < len(w.items) {
		title = fmt.Sprintf("%s  %d–%d", title, start+1, end)
	}

	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		lines = append(lines, w.renderWorkspaceRow(w.items[i], i == w.cursor, contentW))
	}
	return boxWithTitle(title, strings.Join(lines, "\n"), innerW, innerH, true)
}

func (w *welcomeTab) renderWorkspaceRow(it welcomeWorkspaceItem, selected bool, width int) string {
	icon := tabIcon(it.title)
	hotkey := ""
	if it.hotkey != "" {
		hotkey = "[" + it.hotkey + "]"
	}
	hotkeyW := lipgloss.Width(hotkey)
	// cursor(2) + icon(1) + space(1) + title(10) + space(1) + desc + gap + hotkey
	descBudget := width - 2 - 1 - 1 - 10 - 1 - hotkeyW - 1
	desc := ""
	if descBudget >= 6 && it.desc != "" {
		desc = truncateWidth(it.desc, descBudget)
	}

	if selected {
		left := fmt.Sprintf("%s %-10s", icon, truncateWidth(it.title, 10))
		if desc != "" {
			left += " " + desc
		}
		if hotkey != "" {
			pad := maxInt(width-2-lipgloss.Width(left)-hotkeyW, 1)
			left += strings.Repeat(" ", pad) + hotkey
		}
		return cursorLineW(left, true, width)
	}

	iconPart := lipgloss.NewStyle().Foreground(tabIconColor(it.title)).Background(colorSurface).Render(icon)
	titlePart := lipgloss.NewStyle().Foreground(colorFg).Background(colorSurface).Bold(true).
		Render(fmt.Sprintf("%-10s", truncateWidth(it.title, 10)))
	descPart := lipgloss.NewStyle().Foreground(colorMuted).Background(colorSurface).Render(desc)
	rightPart := ""
	if hotkey != "" {
		rightPart = lipgloss.NewStyle().Foreground(colorAccent).Background(colorSurface).Render(hotkey)
	}

	left := cursorPrefix(false) + iconPart + " " + titlePart
	if desc != "" {
		left += " " + descPart
	}
	usedW := lipgloss.Width(left) + hotkeyW
	if hotkey != "" {
		pad := maxInt(width-usedW, 1)
		left += strings.Repeat(" ", pad) + rightPart
	} else if usedW < width {
		left += strings.Repeat(" ", width-usedW)
	}
	return left
}

type shortcutEntry struct {
	key  string
	desc string
}

func (w *welcomeTab) renderShortcutsCard(boxW, boxH int) string {
	innerW := maxInt(boxW-2, 4)
	contentW := maxInt(innerW-2, 1)
	innerH := maxInt(boxH-2, 1)

	headingSt := lipgloss.NewStyle().Foreground(colorAccent).Background(colorSurface).Bold(true)
	navShortcuts := []shortcutEntry{
		{"0", "Home / Welcome page"},
		{"1–9", "Jump to primary tab"},
		{"Tab / S-Tab", "Cycle all tabs"},
		{"^P / ^K", "Fuzzy tab switcher"},
		{",", "Settings (theme & icons)"},
		{"S", "Global cross-type search"},
		{"?", "Full keybinding help"},
	}
	actionShortcuts := []shortcutEntry{
		{"j/k · ↑/↓", "Move cursor"},
		{"h/l · ←/→", "Switch pane focus"},
		{"/", "Filter current list"},
		{"Enter", "View detail / open"},
		{"n / e / d", "New / Edit / Delete"},
		{"ctrl+r", "Refresh current tab"},
		{"q / ^C", "Quit"},
	}

	keyColW := 11
	if contentW < 28 {
		keyColW = 8
	}

	var lines []string
	lines = append(lines, headingSt.Render("Navigation"))
	for _, s := range navShortcuts {
		lines = append(lines, renderShortcutLine(s, keyColW, contentW))
	}
	if len(lines)+2 < innerH {
		lines = append(lines, "")
	}
	lines = append(lines, headingSt.Render("In-Tab Actions"))
	for _, s := range actionShortcuts {
		lines = append(lines, renderShortcutLine(s, keyColW, contentW))
	}
	return boxWithTitle("Common Shortcuts", strings.Join(lines, "\n"), innerW, innerH, false)
}

func renderShortcutLine(s shortcutEntry, keyColW, width int) string {
	kText := truncateWidth(s.key, keyColW)
	kPad := maxInt(keyColW-lipgloss.Width(kText), 0)
	keyRendered := chipKeyStyle.Render(" " + kText + " ") +
		lipgloss.NewStyle().Background(colorSurface).Render(strings.Repeat(" ", kPad+1))
	descBudget := maxInt(width-lipgloss.Width(keyRendered), 1)
	descRendered := lipgloss.NewStyle().Foreground(colorMuted).Background(colorSurface).
		Render(truncateWidth(s.desc, descBudget))
	return keyRendered + descRendered
}

var _ Tab = (*welcomeTab)(nil)
