package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/llm"
)

// tabIcon 返回当前图标集下对应功能的图标（默认 Nerd Font v3，可在 Settings
// 中切换 Unicode / ASCII）。所有图标均在 icons.go 中登记并受白名单保护。
func tabIcon(title string) string {
	pack := activeIconPack()
	if icon, ok := pack.tabs[title]; ok {
		return icon
	}
	return pack.fallback
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
	case "Skill":
		return colorWarn
	case "Config":
		return colorWarn
	case "SSH":
		return colorOk
	case "KeyPair":
		return colorMagenta
	case "MCP":
		return colorCyan
	case "Ledger":
		return colorCyan
	case "Tree":
		return colorOk
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

// renderTab 画一个顶栏菜单项。激活项把 icon + 标题整体渲染为一枚完整强调色
// 胶囊（colorOnAccent on colorAccent），非激活项保留各自彩色图标铺在 surface 上；
// 两种状态宽度严格一致，切 Tab 时菜单不抖动。
func renderTab(title string, active bool) string {
	icon := tabIcon(title)
	if active {
		pillSt := lipgloss.NewStyle().
			Foreground(colorOnAccent).
			Background(colorAccent).
			Bold(true)
		return pillSt.Render(" " + icon + " " + title + " ")
	}
	pad := lipgloss.NewStyle().Foreground(colorFg).Background(colorSurface).Render(" ")
	iconSt := lipgloss.NewStyle().Foreground(tabIconColor(title)).Background(colorSurface)
	titleSt := lipgloss.NewStyle().Foreground(colorMuted).Background(colorSurface)
	return pad + iconSt.Render(icon) + iconSt.Render(" ") + titleSt.Render(title) + pad
}

// renderTabIconOnly 渲染窄屏紧凑模式下的非激活项：只保留带语义色的图标与
// 左右 1 列留白（共 3 列，保证 Nerd Font Proportional 图标有右侧呼吸空间）。
func renderTabIconOnly(title string) string {
	icon := tabIcon(title)
	pad := lipgloss.NewStyle().Foreground(colorFg).Background(colorSurface).Render(" ")
	iconSt := lipgloss.NewStyle().Foreground(tabIconColor(title)).Background(colorSurface)
	return pad + iconSt.Render(icon) + pad
}

// renderTabStrip 画顶栏：左侧 brand `senv`，中间带 icon 的功能菜单，
// 右侧当前 provider 与同步徽标。
//
// 宽度适配分三档：
//  1. 完整模式：全部 Tab 显示「icon + 标题」；
//  2. 紧凑模式：宽屏放不下全称时，非激活 Tab 折叠为「仅图标」、激活 Tab 显示
//     完整「icon + 标题」，14 个 Tab 在 ~80 列也能一屏全览；
//  3. 滑动视口：超窄屏连紧凑模式也放不下时，以激活 Tab 为中心滑动展示相邻
//     项，左右溢出以 ‹ / › 指示。
func (m Model) renderTabStrip(width int) string {
	if width < 1 {
		return ""
	}
	brand := brandStyle.Render("senv")
	separator := tabSeparatorStyle.Render(" ")

	var rightParts []string
	if badge := m.providerBadge(); badge != "" {
		rightParts = append(rightParts, providerBadgeStyle.Render(badge))
	}
	if sync := m.syncBadge(); sync != "" {
		rightParts = append(rightParts, syncBadgeStyle.Render(iconSync()+" "+sync))
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

	titles := make([]string, len(m.tabs))
	for i, t := range m.tabs {
		titles[i] = t.Title()
	}
	mid := fitTabStripTitles(titles, m.active, midBudget, separator)
	left := lipgloss.JoinHorizontal(lipgloss.Top, brand, separator, mid)
	gap := budget - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	row := left + strings.Repeat(" ", gap) + right
	return tabStripStyle.Width(width).Render(row)
}

// fitTabStripTitles 按三档策略把全部 Tab 装进 budget 列，保证激活 Tab 始终可见。
func fitTabStripTitles(titles []string, active, budget int, separator string) string {
	if len(titles) == 0 {
		return ""
	}
	if active < 0 || active >= len(titles) {
		active = 0
	}
	if budget < 4 {
		budget = 4
	}
	sepW := lipgloss.Width(separator)

	// 1. 完整模式：全部显示 icon + title
	fullTabs := make([]string, len(titles))
	fullTotal := 0
	for i, title := range titles {
		fullTabs[i] = renderTab(title, i == active)
		if i > 0 {
			fullTotal += sepW
		}
		fullTotal += lipgloss.Width(fullTabs[i])
	}
	if fullTotal <= budget {
		return joinWithSep(fullTabs, separator)
	}

	// 2. 紧凑模式：激活项完整，其余仅图标（14 Tab 约占 ~65 列）
	compactTabs := make([]string, len(titles))
	compactTotal := 0
	for i, title := range titles {
		if i == active {
			compactTabs[i] = fullTabs[i]
		} else {
			compactTabs[i] = renderTabIconOnly(title)
		}
		if i > 0 {
			compactTotal += sepW
		}
		compactTotal += lipgloss.Width(compactTabs[i])
	}
	if compactTotal <= budget {
		return joinWithSep(compactTabs, separator)
	}

	// 3. 滑动视口：以激活项为锚点，优先保留紧凑图标，向两侧交替扩展。
	activePart := fullTabs[active]
	if lipgloss.Width(activePart) > budget {
		return renderTab(truncateWidth(titles[active], maxInt(budget-4, 1)), true)
	}
	leftArrow := mutedStyle().Background(colorSurface).Render(iconOverflowL())
	rightArrow := mutedStyle().Background(colorSurface).Render(iconOverflowR())
	arrowW := 2 // arrow + separator

	lo, hi := active, active
	used := lipgloss.Width(activePart)
	for lo > 0 || hi < len(titles)-1 {
		grew := false
		// 优先向右扩，再向左扩，让激活项保持视觉居中。
		if hi < len(titles)-1 {
			nextW := sepW + lipgloss.Width(compactTabs[hi+1])
			needArrows := 0
			if lo > 0 {
				needArrows += arrowW
			}
			if hi+1 < len(titles)-1 {
				needArrows += arrowW
			}
			if used+nextW+needArrows <= budget {
				hi++
				used += nextW
				grew = true
			}
		}
		if lo > 0 {
			prevW := sepW + lipgloss.Width(compactTabs[lo-1])
			needArrows := 0
			if lo-1 > 0 {
				needArrows += arrowW
			}
			if hi < len(titles)-1 {
				needArrows += arrowW
			}
			if used+prevW+needArrows <= budget {
				lo--
				used += prevW
				grew = true
			}
		}
		if !grew {
			break
		}
	}

	var out []string
	if lo > 0 {
		out = append(out, leftArrow)
	}
	out = append(out, compactTabs[lo:hi+1]...)
	if hi < len(titles)-1 {
		out = append(out, rightArrow)
	}
	return joinWithSep(out, separator)
}

func joinWithSep(items []string, sep string) string {
	if len(items) == 0 {
		return ""
	}
	if len(items) == 1 {
		return items[0]
	}
	parts := make([]string, 0, len(items)*2-1)
	for i, it := range items {
		if i > 0 {
			parts = append(parts, sep)
		}
		parts = append(parts, it)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
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

// hintBar 把动作列表画成底栏轻量 NAV / ACT 提示条：导航键进 NAV，其余非 NoBar
// 动作进 ACT，尾部固定 `?`。键名用柔和底色 + 强调色粗体，降低底栏视觉噪音。
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
		parts = append(parts, hintGroupStyle.Render("NAV"), strings.Join(nav, "  "))
	}
	parts = append(parts, hintGroupStyle.Render("ACT"), strings.Join(act, "  "))
	return strings.Join(parts, "  ")
}

func actionChip(b KeyAction) string {
	keys := strings.Join(b.Keys, "/")
	if b.Desc == "" {
		return chipKeyStyle.Render(keys)
	}
	return chipKeyStyle.Render(keys) + " " + chipDescStyle.Render(b.Desc)
}
