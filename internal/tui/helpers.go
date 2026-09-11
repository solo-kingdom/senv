package tui

import (
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// 本文件收拢跨 Tab 复用的小工具（tui-ux-list 1.2 自各 Tab 搬入）。

// modalBox 渲染模态弹层：标题 + 正文 + 底部按键提示。
func modalBox(title, body, hint string) string {
	head := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorAccent)).Render("» " + title)
	parts := []string{head}
	if body != "" {
		parts = append(parts, body)
	}
	if hint != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render(hint))
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorAccent)).
		Padding(0, 1).
		Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
	return box
}

// isPrintable 报告按键是否为单个可打印字符输入（用于过滤/输入框回显）。
func isPrintable(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyRunes && len(msg.Runes) == 1
}

// clamp 把 v 限制到 [lo, hi]。
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// maxLen 返回切片长度（nil 安全的语义化写法）。
func maxLen[T any](s []T) int { return len(s) }

// max 返回较大值。
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// orDash 空串显示为 "-"。
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// sortedKeys 返回 map 键的稳定排序。
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// cursorLine 渲染可选中的列表行：统一选中前缀与高亮。
func cursorLine(line string, selected bool) string {
	if selected {
		return selectedLineStyle.Render(cursorPrefix(true) + line)
	}
	return cursorPrefix(false) + line
}
