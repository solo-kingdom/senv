package tui

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// 本文件收拢跨 Tab 复用的小工具（tui-ux-list 1.2 自各 Tab 搬入）。

// modalBox renders a titled modal inside the tab's content area using the same
// rounded boxWithTitle chrome as main panes. w/h are the tab's SetSize
// dimensions: the box border draws outside Width/Height, so inner width is w-2
// and inner height is h, producing an exact w × (h+2) slot.
func modalBox(w, h int, title, body, hint string) string {
	innerW := maxInt(w-2, 4)
	innerH := maxInt(h, 2)
	contentW := maxInt(innerW-2, 1)

	var parts []string
	if body != "" {
		parts = append(parts, lipgloss.NewStyle().Width(contentW).Render(body))
	}
	if hint != "" {
		if len(parts) > 0 {
			parts = append(parts, "")
		}
		parts = append(parts, lipgloss.NewStyle().Foreground(colorMuted).Width(contentW).Render(hint))
	}
	return clipLines(boxWithTitle(title, strings.Join(parts, "\n"), innerW, innerH, true), maxInt(h+2, 2))
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

// max 返回较大值。
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// minInt 返回较小值。
func minInt(a, b int) int {
	if a < b {
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
	return cursorLineW(line, selected, 0)
}

// cursorLineW 在 width>0 时把选中行铺满整行（背景填充到列宽）。
// 未选中行也铺上主题前景和 surface 底，避免浅色主题继承终端默认浅色字。
func cursorLineW(line string, selected bool, width int) string {
	line = cursorPrefix(selected) + line
	if !selected {
		if width > 0 {
			line = padRight(truncateWidth(line, width), width)
		}
		return unselectedLineStyle.Render(line)
	}
	return selectedRow(line, width)
}

// selectedRow 用选中样式渲染一行；width>0 时先截断再右补齐，形成整行色条。
func selectedRow(line string, width int) string {
	if width > 0 {
		line = padRight(truncateWidth(line, width), width)
	}
	return selectedLineStyle.Render(line)
}
