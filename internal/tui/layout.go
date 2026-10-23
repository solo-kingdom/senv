package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// frameLayout 是整帧布局的唯一权威：由 WindowSizeMsg 一次算出每个区域的
// 固定矩形，View 只负责把各区域内容「装箱」进去。
//
//	row 0            外框上边
//	row 1..2         tab 栏（标签行 + 下划线）        stripH = 2
//	row 3..3+bodyH-1 内容区（Tab 视图 / 搜索 / 帮助）  bodyH = H - 5
//	row H-2          底栏（状态 / 错误 / 提示）        footerH = 1
//	row H-1          外框下边
//
// 每个区域的输出都经 fitBlock 硬裁剪 + 补齐到 innerW × 区域高，再由
// renderFrame 拼上左右边框，因此整帧恒为 W 列 × H 行，与 Tab 内部渲染
// 是否正确无关（Tab 写错只会在自己的格子里被截断，不会把别的区域挤走，
// 更不会让终端滚屏留下「双 header」残影）。
type frameLayout struct {
	W, H   int // 终端尺寸
	innerW int // 外框内宽 = W - 2
	stripH int // tab 栏高
	bodyH  int // 内容区高（含 Tab 自己的面板边框）
	footH  int // 底栏高
}

const (
	layoutStripH = 2
	layoutFootH  = 1
	// layoutMinW/H 是能画出完整 chrome 的最小终端尺寸；更小时只显示提示。
	layoutMinW = 30
	layoutMinH = 7
)

// computeLayout 由终端尺寸推出各区域矩形。
func computeLayout(w, h int) frameLayout {
	l := frameLayout{W: w, H: h, stripH: layoutStripH, footH: layoutFootH}
	l.innerW = maxInt(w-2, 1)
	l.bodyH = maxInt(h-2-layoutStripH-layoutFootH, 0)
	return l
}

// tooSmall 报告终端是否小于最小可用尺寸。
func (l frameLayout) tooSmall() bool { return l.W < layoutMinW || l.H < layoutMinH }

// tabSize 是下发给 Tab.SetSize 的尺寸。历史约定：Tab 面板的边框画在
// Width/Height 之外，所以 Tab 拿到的高度比内容区少 2 行（Tab 渲染
// height+2 行恰好填满内容区）。
func (l frameLayout) tabSize() (w, h int) {
	return l.innerW, maxInt(l.bodyH-2, 0)
}

// frameBorder 是外框边线样式（颜色取自调色板）。
var frameBorder = lipgloss.RoundedBorder()

// renderFrame 把三个区域装箱并套上外框，输出恰好 W×H。
func (l frameLayout) renderFrame(strip, body, footer string) string {
	bs := frameBorderStyle()
	rows := make([]string, 0, l.H)
	horiz := strings.Repeat(frameBorder.Top, l.innerW)
	rows = append(rows, bs.Render(frameBorder.TopLeft+horiz+frameBorder.TopRight))
	left, right := bs.Render(frameBorder.Left), bs.Render(frameBorder.Right)
	for _, region := range []struct {
		s string
		h int
	}{{strip, l.stripH}, {body, l.bodyH}, {footer, l.footH}} {
		for _, line := range fitBlock(region.s, l.innerW, region.h) {
			rows = append(rows, left+line+right)
		}
	}
	rows = append(rows, bs.Render(frameBorder.BottomLeft+
		strings.Repeat(frameBorder.Bottom, l.innerW)+frameBorder.BottomRight))
	// 最后一道闸：任何情况下都按终端尺寸收口（含 H 很小时的裁剪）。
	return strings.Join(fitBlock(strings.Join(rows, "\n"), l.W, l.H), "\n")
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
