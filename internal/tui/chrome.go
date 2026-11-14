package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/llm"
)

// tabIcon 返回功能菜单项的宽度稳定符号（两种宽度模型一致、且不是
// Nerd Font 私用区，避免被 sanitizeLine 折成 "?"）。
func tabIcon(title string) string {
	switch title {
	case "Env":
		return "◆"
	case "Text":
		return "¶"
	case "Providers":
		return "◈"
	case "Config":
		return "✎"
	case "SSH":
		return "⌘"
	case "KeyPair":
		return "⋆"
	case "MCP":
		return "⬡"
	case "History":
		return "↺"
	case "Audit":
		return "⚑"
	case "Backup":
		return "▣"
	case "Settings":
		return "⚙"
	default:
		return "•"
	}
}

// tabLabel 是顶栏菜单里显示的「icon + 空格 + 标题」。
func tabLabel(title string) string {
	if icon := tabIcon(title); icon != "" {
		return icon + " " + title
	}
	return title
}

// tabIconColor 给每个功能一个调色板角色，图标在 surface 上保持
// 与正文相同的对比度下限（角色本身已由 TestPaletteContrast 约束）。
func tabIconColor(title string) lipgloss.TerminalColor {
	switch title {
	case "Env":
		return colorCyan
	case "Text":
		return colorAccent
	case "Providers":
		return colorMagenta
	case "Config":
		return colorWarn
	case "SSH":
		return colorOk
	case "KeyPair":
		return colorMagenta
	case "MCP":
		return colorCyan
	case "History":
		return colorAccent
	case "Audit":
		return colorWarn
	case "Backup":
		return colorOk
	case "Settings":
		return colorAccent
	default:
		return colorMuted
	}
}

// renderTab 画一个顶栏菜单项。图标用各自的颜色铺在 surface 上；激活项
// 只把标题做成强调色胶囊，避免彩色图标压在强调底上对比度不够。左右
// 各留 1 列空白，宽度与原先 Padding(0,1)+tabLabel 一致。
func renderTab(title string, active bool) string {
	icon := tabIcon(title)
	pad := lipgloss.NewStyle().Foreground(colorFg).Background(colorSurface).Render(" ")
	iconSt := lipgloss.NewStyle().Foreground(tabIconColor(title)).Background(colorSurface)
	if active {
		titleSt := lipgloss.NewStyle().
			Foreground(colorOnAccent).
			Background(colorAccent).
			Bold(true)
		return pad + iconSt.Render(icon) + iconSt.Render(" ") + titleSt.Render(title) + pad
	}
	titleSt := lipgloss.NewStyle().Foreground(colorMuted).Background(colorSurface)
	return pad + iconSt.Render(icon) + iconSt.Render(" ") + titleSt.Render(title) + pad
}

// renderTabStrip 画顶栏：左侧 brand `senv`，中间带 icon 的功能菜单，
// 右侧当前 provider 与同步徽标。预算不够时从右侧截断菜单，保留 brand
// 与右侧徽标，并保证激活项仍可见。
func (m Model) renderTabStrip(width int) string {
	if width < 1 {
		return ""
	}
	brand := brandStyle.Render("senv")
	separator := tabSeparatorStyle.Render(" ")
	tabParts := make([]string, 0, len(m.tabs)*2-1)
	for i, t := range m.tabs {
		if i > 0 {
			tabParts = append(tabParts, separator)
		}
		tabParts = append(tabParts, renderTab(t.Title(), i == m.active))
	}

	var rightParts []string
	if badge := m.providerBadge(); badge != "" {
		rightParts = append(rightParts, providerBadgeStyle.Render(badge))
	}
	if sync := m.syncBadge(); sync != "" {
		rightParts = append(rightParts, syncBadgeStyle.Render(sync))
	}
	right := ""
	if len(rightParts) > 0 {
		right = lipgloss.JoinHorizontal(lipgloss.Top, joinSpaced(rightParts)...)
	}

	// tabStripStyle 的 Padding(0,1) 占 2 列，内容预算为 width-2。
	budget := maxInt(width-2, 1)
	leftW, rightW := lipgloss.Width(brand), lipgloss.Width(right)
	// brand 与右侧徽标之间至少留 1 列空隙；菜单预算是两者中间。
	gapKeep := 1
	if rightW == 0 {
		gapKeep = 0
	}
	midBudget := budget - leftW - 1 - rightW - gapKeep
	if midBudget < 4 {
		// 优先保住 brand 与激活项，徽标再让路。
		keepRight := minInt(rightW, maxInt(budget-leftW-6, 0))
		if rightW > keepRight {
			right = truncateWidth(right, keepRight)
			rightW = lipgloss.Width(right)
		}
		midBudget = maxInt(budget-leftW-1-rightW-gapKeep, 4)
	}
	active := m.tabs[m.active].Title()
	mid := fitTabStrip(tabParts, active, m.active, midBudget)
	left := lipgloss.JoinHorizontal(lipgloss.Top, brand, tabSeparatorStyle.Render(" "), mid)
	gap := budget - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	row := left + strings.Repeat(" ", gap) + right
	return tabStripStyle.Width(width).Render(row)
}

// fitTabStrip 把已经渲染好的 tab 片段装进 budget 列。放不下的尾部以 "…"
// 收尾；若激活 tab 会被截掉，则退化为只渲染激活 tab。
func fitTabStrip(parts []string, activeTitle string, active, budget int) string {
	if budget < 4 {
		budget = 4
	}
	var out []string
	used := 0
	cut := -1
	for i, p := range parts {
		w := lipgloss.Width(p)
		if used+w > budget {
			cut = i
			break
		}
		out = append(out, p)
		used += w
	}
	if cut < 0 {
		return lipgloss.JoinHorizontal(lipgloss.Top, out...)
	}
	// 激活 tab（parts 下标 2*active）被截掉时，退化为只渲染激活 tab。
	if 2*active >= cut {
		// budget 至少要留给左右 padding、图标和空格（4 列），其余给标题。
		return renderTab(truncateWidth(activeTitle, maxInt(budget-4, 1)), true)
	}
	if used+1 <= budget {
		out = append(out, mutedStyle().Background(colorSurface).Render("…"))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, out...)
}

func joinSpaced(parts []string) []string {
	if len(parts) <= 1 {
		return parts
	}
	out := make([]string, 0, len(parts)*2-1)
	for i, p := range parts {
		if i > 0 {
			out = append(out, " ")
		}
		out = append(out, p)
	}
	return out
}

// refreshLLMChrome 读取 agent 指针快照，供顶栏 provider 徽标使用。
// 只读本地文件，不访问网络；LLM 未配置或读取失败时清空缓存。
func (m *Model) refreshLLMChrome() {
	m.llmChrome = nil
	if m.mgr.LLM == nil {
		return
	}
	sm := llm.NewSwitchManager(m.mgr.LLM, m.mgr.LLMPointer, m.mgr.LLMHome)
	rows, _, err := sm.Status()
	if err != nil {
		return
	}
	m.llmChrome = rows
}

// providerBadge 取最近一次切换的 provider 别名；没有任何指针时为空。
func (m Model) providerBadge() string {
	var latest string
	var at time.Time
	for _, row := range m.llmChrome {
		if row.Pointer == nil || row.Pointer.Provider == "" {
			continue
		}
		t, err := row.Pointer.SwitchedAtTime()
		if err != nil {
			if latest == "" {
				latest = row.Pointer.Provider
			}
			continue
		}
		if latest == "" || t.After(at) {
			latest, at = row.Pointer.Provider, t
		}
	}
	return latest
}

// hintBar 把动作列表画成底栏 NAV / ACT 芯片：导航键进 NAV，其余非 NoBar
// 动作进 ACT，尾部固定 `?`。groupBar 仍保留给单元测试与纯文本消费方。
func hintBar(bindings []KeyAction) string {
	var nav, act []string
	for _, b := range bindings {
		if b.Group == grpNav {
			nav = append(nav, actionChip(b))
			continue
		}
		if b.NoBar {
			continue
		}
		act = append(act, actionChip(b))
	}
	act = append(act, actionChip(KeyAction{Keys: []string{"?"}, Desc: "help"}))
	var parts []string
	if len(nav) > 0 {
		parts = append(parts, hintGroupStyle.Render("NAV"), strings.Join(nav, " "))
	}
	parts = append(parts, hintGroupStyle.Render("ACT"), strings.Join(act, " "))
	return strings.Join(parts, "  ")
}

func actionChip(b KeyAction) string {
	keys := strings.Join(b.Keys, "/")
	if b.Desc == "" {
		return chipKeyStyle.Render(keys)
	}
	return chipKeyStyle.Render(keys) + " " + chipDescStyle.Render(b.Desc)
}
