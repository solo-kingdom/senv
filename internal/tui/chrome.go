package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/llm"
)

// renderNav 把现有 Tab 画成左侧垂直菜单：激活项用 accent 胶囊铺满整行，
// 其余为 muted 文本。条目多于高度时以激活项为中心做窗口，Tab/数字键
// 仍可切到被收起的项。不引入独立的导航焦点，以免抢走分组侧栏的 h/l。
func renderNav(tabs []Tab, active, width, height int) string {
	if width < 1 || height < 1 || len(tabs) == 0 {
		return ""
	}
	if active < 0 {
		active = 0
	}
	if active >= len(tabs) {
		active = len(tabs) - 1
	}
	start, end := visibleRange(len(tabs), active, height)
	lines := make([]string, 0, end-start)
	inner := maxInt(width, 1)
	for i := start; i < end; i++ {
		label := truncateWidth(tabs[i].Title(), inner)
		if i == active {
			lines = append(lines, activeNavPill(label, inner))
		} else {
			lines = append(lines, tabStyle.Width(inner).MaxHeight(1).Background(colorSurface).Render(label))
		}
	}
	block := strings.Join(lines, "\n")
	return navPaneStyle.Width(width).Height(height).MaxHeight(height).Render(block)
}

// activeNavPill 渲染左侧导航的激活胶囊，铺满 nav 列宽。测试用同一函数断言，
// 避免 Width/MaxHeight 与 View 输出对不上。
func activeNavPill(label string, width int) string {
	if width < 1 {
		width = 1
	}
	return activeTabStyle.Width(width).MaxHeight(1).Render(label)
}

// refreshLLMChrome 读取 agent 指针快照，供顶栏胶囊与 provider 徽标使用。
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

// renderTopBar 画顶栏：左侧 brand `senv` 与 agent 胶囊，右侧当前 provider
// 与同步徽标。预算不够时从右侧截断胶囊，保留 brand 与右侧徽标。
func (m Model) renderTopBar(width int) string {
	if width < 1 {
		return ""
	}
	brand := brandStyle.Render("senv")
	leftParts := []string{brand}
	if pills := m.renderAgentPills(); pills != "" {
		leftParts = append(leftParts, mutedStyle().Render("│"), pills)
	}
	left := lipgloss.JoinHorizontal(lipgloss.Top, leftParts...)

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

	// topBarStyle 的 Padding(0,1) 占 2 列，内容预算为 width-2。
	budget := maxInt(width-2, 1)
	leftW, rightW := lipgloss.Width(left), lipgloss.Width(right)
	if leftW+rightW+1 > budget {
		// 优先保住 brand 与右侧徽标，再尽量保留胶囊。
		keepRight := minInt(rightW, maxInt(budget-lipgloss.Width(brand)-1, 0))
		if rightW > keepRight {
			right = truncateWidth(right, keepRight)
			rightW = lipgloss.Width(right)
		}
		keepLeft := maxInt(budget-rightW-1, 1)
		if leftW > keepLeft {
			left = truncateWidth(left, keepLeft)
			leftW = lipgloss.Width(left)
		}
	}
	gap := budget - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	row := left + strings.Repeat(" ", gap) + right
	return topBarStyle.Width(width).Render(row)
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

func (m Model) renderAgentPills() string {
	if len(m.llmChrome) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m.llmChrome)*2-1)
	for i, row := range m.llmChrome {
		if i > 0 {
			parts = append(parts, " ")
		}
		label := row.AgentID
		if label == "" {
			label = row.AgentName
		}
		if row.Pointer != nil && row.Pointer.Provider != "" {
			parts = append(parts, agentPillOnStyle.Render(label))
		} else {
			parts = append(parts, agentPillStyle.Render(label))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
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
