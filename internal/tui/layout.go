package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// frameLayout 是整帧布局的唯一权威：由 WindowSizeMsg 一次算出每个区域的
// 固定矩形，View 只负责把各区域内容「装箱」进去。
//
//	row 0            外框上边
//	row 1..2         顶栏（brand / agent 胶囊 / provider+同步 + 下划线）  topH = 2
//	row 3..3+bodyH-1 内容区：左侧导航 | Tab 视图 / 搜索 / 帮助         bodyH = H - 5
//	row H-2          底栏（状态 / 错误 / NAV+ACT 提示）                footerH = 1
//	row H-1          外框下边
//
// 每个区域的输出都经 fitBlock 硬裁剪 + 补齐到既定宽 × 高，再由
// renderFrame 拼上左右边框与导航分隔线，因此整帧恒为 W 列 × H 行，与
// Tab 内部渲染是否正确无关（Tab 写错只会在自己的格子里被截断，不会把
// 别的区域挤走，更不会让终端滚屏留下「双 header」残影）。
type frameLayout struct {
	W, H     int // 终端尺寸
	innerW   int // 外框内宽 = W - 2
	topH     int // 顶栏高（内容行 + 下划线）
	bodyH    int // 内容区高（含 Tab 自己的面板边框）
	footH    int // 底栏高
	navW     int // 左侧导航宽（不含分隔线）
	contentW int // Tab / 浮层内容宽 = innerW - navW - 1
}

const (
	layoutTopH  = 2
	layoutFootH = 1
	// layoutNavW 是左侧导航的默认内宽，按最长标签（KeyPair / History）
	// 加左右内边距估算；窄终端由 computeLayout 收缩。
	layoutNavW = 14
	// layoutMinW/H 是能画出完整 chrome 的最小终端尺寸；更小时只显示提示。
	// 高度预算与历史约定一致：顶栏 2 + 底栏 1 + 外框 2 = 5，最小可用 7 行。
	layoutMinW = 30
	layoutMinH = 7
	// layoutMinContentW 是把导航收缩后仍给内容区留下的下限。
	layoutMinContentW = 8
	layoutMinNavW     = 8
)

// computeLayout 由终端尺寸推出各区域矩形。
func computeLayout(w, h int) frameLayout {
	l := frameLayout{W: w, H: h, topH: layoutTopH, footH: layoutFootH}
	l.innerW = maxInt(w-2, 1)
	l.bodyH = maxInt(h-2-layoutTopH-layoutFootH, 0)
	nav := layoutNavW
	if l.innerW < nav+1+layoutMinContentW {
		nav = l.innerW - 1 - layoutMinContentW
	}
	if nav < layoutMinNavW {
		nav = layoutMinNavW
	}
	if nav+1 >= l.innerW {
		nav = maxInt(l.innerW-1-layoutMinContentW, 1)
	}
	l.navW = nav
	l.contentW = maxInt(l.innerW-l.navW-1, 1)
	return l
}

// tooSmall 报告终端是否小于最小可用尺寸。
func (l frameLayout) tooSmall() bool { return l.W < layoutMinW || l.H < layoutMinH }

// tabSize 是下发给 Tab.SetSize 的尺寸。历史约定：Tab 面板的边框画在
// Width/Height 之外，所以 Tab 拿到的高度比内容区少 2 行（Tab 渲染
// height+2 行恰好填满内容区）。宽度是内容区（导航右侧），不再是整行内宽。
func (l frameLayout) tabSize() (w, h int) {
	return l.contentW, maxInt(l.bodyH-2, 0)
}

// frameBorder 是外框边线样式（颜色取自调色板）。
var frameBorder = lipgloss.RoundedBorder()

// renderFrame 把顶栏、左侧导航、内容区、底栏装箱并套上外框，输出恰好 W×H。
func (l frameLayout) renderFrame(top, nav, body, footer string) string {
	return l.assemble(top, footer, true, nav, body)
}

// renderFrameWide 在搜索 / 帮助 / 确认浮层打开时使用：内容区占满内宽，
// 导航暂时让出空间，浮层仍按历史的整帧宽度排版（两列键位、居中框体）。
func (l frameLayout) renderFrameWide(top, body, footer string) string {
	return l.assemble(top, footer, false, "", body)
}

func (l frameLayout) assemble(top, footer string, withNav bool, nav, body string) string {
	bs := frameBorderStyle()
	rows := make([]string, 0, l.H)
	horiz := strings.Repeat(frameBorder.Top, l.innerW)
	rows = append(rows, bs.Render(frameBorder.TopLeft+horiz+frameBorder.TopRight))
	left, right := bs.Render(frameBorder.Left), bs.Render(frameBorder.Right)
	div := bs.Render(frameBorder.Left)
	for _, line := range fitBlock(top, l.innerW, l.topH) {
		rows = append(rows, left+line+right)
	}
	if withNav {
		navLines := fitBlock(nav, l.navW, l.bodyH)
		bodyLines := fitBlock(body, l.contentW, l.bodyH)
		for i := 0; i < l.bodyH; i++ {
			rows = append(rows, left+navLines[i]+div+bodyLines[i]+right)
		}
	} else {
		for _, line := range fitBlock(body, l.innerW, l.bodyH) {
			rows = append(rows, left+line+right)
		}
	}
	for _, line := range fitBlock(footer, l.innerW, l.footH) {
		rows = append(rows, left+line+right)
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
