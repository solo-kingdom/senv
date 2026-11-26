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
	case "Home":
		return colorAccent
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

// allNavTitles 返回顶栏展示的全部菜单标题（Home 置首，后接全部已注册 Tab）
// 以及当前激活项在其中的下标。所有菜单均常驻可见，不再隐藏低频项。
func (m Model) allNavTitles() (titles []string, active int) {
	hasHome := m.welcome != nil
	offset := 0
	if hasHome {
		titles = make([]string, 0, len(m.tabs)+1)
		titles = append(titles, m.welcome.Title())
		offset = 1
	} else {
		titles = make([]string, 0, len(m.tabs))
	}
	for _, t := range m.tabs {
		titles = append(titles, t.Title())
	}
	if hasHome && m.onWelcome {
		return titles, 0
	}
	active = offset + m.active
	if active < 0 || active >= len(titles) {
		active = 0
	}
	return titles, active
}

// tabStripHeight 计算在 (width, height) 终端下顶栏应占的总行数（菜单行 + 下划线）。
// 宽屏单行全显时为 2；中屏两行均衡全显时为 3；极窄/极矮终端回落单行紧凑模式为 2。
func (m Model) tabStripHeight(termW, termH int) int {
	innerW := maxInt(termW-2, 1)
	budget := maxInt(innerW-2, 1)
	titles, _ := m.allNavTitles()
	if len(titles) == 0 {
		return layoutTopH
	}
	if totalTabsWidth(titles) <= budget {
		return layoutTopH
	}
	if termH >= 12 {
		if _, ok := splitBalancedTwoRows(titles, budget); ok {
			return layoutTopH + 1
		}
	}
	return layoutTopH
}

// renderHeaderRight 渲染嵌入外框右上边的 provider 与同步状态徽标（按健康状态着色）。
func (m Model) renderHeaderRight() string {
	var rightParts []string
	if badge := m.providerBadge(); badge != "" {
		rightParts = append(rightParts, providerBadgeStyle.Render(badge))
	}
	if sync := m.syncBadge(); sync != "" {
		st := syncBadgeOkStyle
		switch {
		case m.syncState.Err != nil:
			st = syncBadgeErrStyle
		case m.syncState.Dirty > 0:
			st = syncBadgeWarnStyle
		}
		rightParts = append(rightParts, st.Render(iconSync()+" "+sync))
	}
	if len(rightParts) == 0 {
		return ""
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, joinSpaced(rightParts)...)
}

// renderTabStrip 画全量功能菜单顶栏（Senv 品牌名与状态徽标已上移至外框上边，
// 顶栏 100% 宽度留给功能菜单，不再隐藏任何 Tab）。
//
// 宽度适配分三档：
//  1. 单行完整模式：全部 Tab 显示「icon + 标题」排成一行；
//  2. 双行均衡模式：单行放不下且终端高度充足时，将全部 Tab 均衡折成两行，
//     每一项都保留完整「icon + 标题」，无隐藏菜单；
//  3. 紧凑 / 滑动模式：超窄或超矮终端下回落单行，非激活项折叠为仅图标，
//     再放不下时以激活项为中心滑动展示并以 ‹ / › 指示溢出。
func (m Model) renderTabStrip(width, termH int) string {
	if width < 1 {
		return ""
	}
	// tabStripStyle 的 Padding(0,1) 占 2 列，内容预算为 width-2。
	budget := maxInt(width-2, 1)
	titles, active := m.allNavTitles()
	if len(titles) == 0 {
		return tabStripStyle.Width(width).Render("")
	}

	// 1. 单行完整模式
	if totalTabsWidth(titles) <= budget {
		row := renderFullTabRow(titles, active, 0)
		return tabStripStyle.Width(width).Render(row)
	}

	// 2. 双行均衡完整模式（全部 Tab 可见，无隐藏）
	if termH >= 12 {
		if split, ok := splitBalancedTwoRows(titles, budget); ok {
			row1 := renderFullTabRow(titles[:split], active, 0)
			row2 := renderFullTabRow(titles[split:], active, split)
			body := lipgloss.JoinVertical(lipgloss.Left, row1, row2)
			return tabStripStyle.Width(width).Render(body)
		}
	}

	// 3. 超窄/超矮终端回落单行紧凑或滑动视口
	mid := fitTabStripTitles(titles, active, budget, "")
	return tabStripStyle.Width(width).Render(mid)
}

func totalTabsWidth(titles []string) int {
	total := 0
	for _, title := range titles {
		total += lipgloss.Width(renderTab(title, false))
	}
	return total
}

// splitBalancedTwoRows 把 titles 拆成两行，要求两行宽度都不超过 budget，
// 且尽量均衡（最小化 max(w1, w2)）。拆不开时返回 ok=false。
func splitBalancedTwoRows(titles []string, budget int) (split int, ok bool) {
	if len(titles) < 2 {
		return 0, false
	}
	widths := make([]int, len(titles))
	total := 0
	for i, title := range titles {
		w := lipgloss.Width(renderTab(title, false))
		widths[i] = w
		total += w
	}
	bestSplit := -1
	bestMax := 0
	prefix := 0
	for i := 1; i < len(titles); i++ {
		prefix += widths[i-1]
		suffix := total - prefix
		if prefix <= budget && suffix <= budget {
			m := maxInt(prefix, suffix)
			if bestSplit == -1 || m < bestMax {
				bestSplit = i
				bestMax = m
			}
		}
	}
	if bestSplit == -1 {
		return 0, false
	}
	return bestSplit, true
}

func renderFullTabRow(titles []string, activeGlobal, offset int) string {
	parts := make([]string, len(titles))
	for i, title := range titles {
		parts[i] = renderTab(title, offset+i == activeGlobal)
	}
	return joinWithSep(parts, "")
}

// fitTabStripTitles 按三档策略把全部 Tab 装进单行 budget 列，保证激活 Tab 始终可见。
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

	// 2. 紧凑模式：激活项完整，其余仅图标（15 Tab 约占 ~52 列）
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
