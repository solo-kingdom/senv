package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// frameLayout 是整帧布局的唯一权威：由 WindowSizeMsg 一次算出每个区域的
// 固定矩形，View 只负责把各区域内容「装箱」进去。
//
//	row 0                 外框上边（嵌入 Senv 标题 + 右侧状态徽标）
//	row 1..topH           顶栏（带 icon 的全量功能菜单，1–2 行自适应 + 下划线）
//	row topH+1..H-3       内容区（欢迎页 / 当前 Tab / 搜索 / 帮助）
//	row H-2               底栏（状态 / 错误 / NAV+ACT 提示）                footerH = 1
//	row H-1               外框下边
//
// 每个区域的输出都经 fitBlock 硬裁剪 + 补齐到既定宽 × 高，再由
// renderFrameChrome 拼上左右边框，因此整帧恒为 W 列 × H 行，与 Tab 内部渲染
// 是否正确无关（Tab 写错只会在自己的格子里被截断，不会把别的区域挤走，
// 更不会让终端滚屏留下「双 header」残影）。
type frameLayout struct {
	W, H   int // 终端尺寸
	innerW int // 外框内宽 = W - 2
	topH   int // 顶栏高（菜单行 + 下划线，1 行菜单为 2，2 行菜单为 3）
	bodyH  int // 内容区高（含 Tab 自己的面板边框）
	footH  int // 底栏高
}

const (
	layoutTopH  = 2
	layoutFootH = 1
	// layoutMinW/H 是能画出完整 chrome 的最小终端尺寸；更小时只显示提示。
	// 高度预算：顶栏 2 + 底栏 1 + 外框 2 = 5，最小可用 7 行。
	layoutMinW = 30
	layoutMinH = 7
)

// computeLayout 由终端尺寸推出单行顶栏的各区域矩形。
func computeLayout(w, h int) frameLayout {
	return computeLayoutFor(w, h, layoutTopH)
}

// computeLayoutFor 由终端尺寸与顶栏高度推出各区域矩形。
func computeLayoutFor(w, h, topH int) frameLayout {
	if topH < layoutTopH {
		topH = layoutTopH
	}
	l := frameLayout{W: w, H: h, topH: topH, footH: layoutFootH}
	l.innerW = maxInt(w-2, 1)
	l.bodyH = maxInt(h-2-topH-layoutFootH, 0)
	return l
}

// tooSmall 报告终端是否小于最小可用尺寸。
func (l frameLayout) tooSmall() bool { return l.W < layoutMinW || l.H < layoutMinH }

// tabSize 是下发给 Tab.SetSize 的尺寸。历史约定：Tab 面板的边框画在
// Width/Height 之外，所以 Tab 拿到的高度比内容区少 2 行（Tab 渲染
// height+2 行恰好填满内容区）。宽度是整行内宽。
func (l frameLayout) tabSize() (w, h int) {
	return l.innerW, maxInt(l.bodyH-2, 0)
}

// frameBorder 是外框边线样式（颜色取自调色板）。
var frameBorder = lipgloss.RoundedBorder()

// renderFrame 把顶栏、内容区、底栏装箱并套上带 Senv 标题的外框，输出恰好 W×H。
func (l frameLayout) renderFrame(top, body, footer string) string {
	return l.renderFrameChrome("Senv", "", top, body, footer)
}

// renderFrameChrome 把顶栏、内容区、底栏装箱并套上外框：左上边框嵌入标题（如
// `╭ Senv ──...`），右上边框按剩余空间嵌入状态徽标（provider / sync），输出恰好 W×H。
func (l frameLayout) renderFrameChrome(title, rightBadge, top, body, footer string) string {
	bs := frameBorderStyle()
	rows := make([]string, 0, l.H)
	rows = append(rows, l.renderTopBorder(bs, title, rightBadge))
	left, right := bs.Render(frameBorder.Left), bs.Render(frameBorder.Right)
	for _, line := range fitBlock(top, l.innerW, l.topH) {
		rows = append(rows, left+line+right)
	}
	for _, line := range fitBlock(body, l.innerW, l.bodyH) {
		rows = append(rows, left+line+right)
	}
	for _, line := range fitBlock(footer, l.innerW, l.footH) {
		rows = append(rows, left+line+right)
	}
	rows = append(rows, bs.Render(frameBorder.BottomLeft+
		strings.Repeat(frameBorder.Bottom, l.innerW)+frameBorder.BottomRight))
	// 最后一道闸：任何情况下都按终端尺寸收口（含 H 很小时的裁剪）。
	return strings.Join(fitBlock(strings.Join(rows, "\n"), l.W, l.H), "\n")
}

// renderTopBorder 画外框上边：左侧嵌入品牌名 `╭ Senv `（与 boxWithTitle 卡片风格一致），
// 右侧空间充足时嵌入 provider / sync 徽标 ` badge ─╮`。
func (l frameLayout) renderTopBorder(bs lipgloss.Style, title, rightBadge string) string {
	surfaceSpace := lipgloss.NewStyle().Foreground(colorFg).Background(colorSurface).Render(" ")
	titlePart := ""
	titleW := 0
	if strings.TrimSpace(title) != "" && l.innerW >= 4 {
		t := " " + title + " "
		if lipgloss.Width(t) > l.innerW {
			t = " " + truncateWidth(title, maxInt(l.innerW-2, 1)) + " "
		}
		titleW = lipgloss.Width(t)
		titlePart = brandStyle.Render(t)
	}

	rightBadge = strings.TrimSpace(rightBadge)
	if rightBadge != "" {
		rw := lipgloss.Width(rightBadge)
		// 保留：titleW + 至少 1 个 ─ + " " + rightBadge + " " + 1 个 ─
		if titleW+rw+4 <= l.innerW {
			midDashes := l.innerW - titleW - rw - 3
			return bs.Render(frameBorder.TopLeft) +
				titlePart +
				bs.Render(strings.Repeat(frameBorder.Top, midDashes)) +
				surfaceSpace + rightBadge + surfaceSpace +
				bs.Render(frameBorder.Top+frameBorder.TopRight)
		}
	}

	dashes := maxInt(l.innerW-titleW, 0)
	return bs.Render(frameBorder.TopLeft) +
		titlePart +
		bs.Render(strings.Repeat(frameBorder.Top, dashes)+frameBorder.TopRight)
}

// renderTooSmall 渲染最小尺寸提示，同样钉死为 W×H（不画边框）。
func (l frameLayout) renderTooSmall() string {
	hint := "terminal too small (need ≥30×7 chars)"
	lines := make([]string, maxInt(l.H, 1))
	mid := maxInt((l.H-1)/2, 0)
	if mid < len(lines) {
		pad := maxInt((l.W-lipgloss.Width(hint))/2, 0)
		lines[mid] = strings.Repeat(" ", pad) + hint
	}
	return strings.Join(fitBlock(strings.Join(lines, "\n"), maxInt(l.W, 1), maxInt(l.H, 1)), "\n")
}
