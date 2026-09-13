package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// globalKeys 是顶层 model 处理的键，对每个 Tab 生效；Tab 专属键位来自该
// Tab 的 Bindings()（keymap 注册表），因此总览与实际行为同源、不可能漂移。
var globalKeys = []KeyAction{
	{[]string{"1–9"}, "jump to tab", ""},
	{[]string{"Tab / Shift+Tab"}, "cycle tabs", ""},
	{[]string{"S"}, "global search", ""},
	{[]string{"ctrl+r"}, "refresh current tab", ""},
	{[]string{"?"}, "keybinding overview", ""},
	{[]string{"esc"}, "back: clear filter / close overlay / wizard back", ""},
	{[]string{"q / ctrl+c"}, "quit (q warns once with pending pushes)", ""},
}

// helpTab is the keybinding overview overlay (triggered by `?`).
//
// Unlike the search overlay it renders as a floating window on top of the
// active tab's content: while open it captures every key, and `?`/`esc` close
// it. Layout is height-first: bindings flow down a single column until the
// rows exceed the box height, then a second column takes the remainder (at
// most two columns, however wide the terminal is), with a visible divider
// between columns. A description longer than its column wraps at word
// boundaries, continuation lines aligned under the description; the laid-out
// rows scroll when they still exceed the box height.
type helpTab struct {
	title    string
	bindings []KeyAction
	width    int
	height   int
	scroll   int
}

// helpCloseMsg asks the top-level model to close the help overlay.
type helpCloseMsg struct{}

// newHelpTab builds the overlay for a tab. Tab-specific rows come straight
// from the tab's keymap bindings, so the overlay and the status bar cannot
// drift from what the tab actually handles.
func newHelpTab(title string, tab Tab) *helpTab {
	return &helpTab{title: title, bindings: tab.Bindings()}
}

func (h *helpTab) Title() string         { return "Help" }
func (h *helpTab) Bindings() []KeyAction { return nil }
func (h *helpTab) InputMode() bool       { return true }
func (h *helpTab) Init() tea.Cmd         { return nil }
func (h *helpTab) Reload() tea.Cmd       { return nil }
func (h *helpTab) SetSize(w, hh int)     { h.width, h.height = w, hh }

func (h *helpTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return h, nil
	}
	switch key.String() {
	case "?", "esc":
		return h, func() tea.Msg { return helpCloseMsg{} }
	case "up", "k":
		h.scroll--
	case "down", "j":
		h.scroll++
	case "pgup":
		h.scroll -= h.visibleRows()
	case "pgdown":
		h.scroll += h.visibleRows()
	case "g", "home":
		h.scroll = 0
	case "G", "end":
		h.scroll = h.maxScroll()
	}
	h.scroll = clamp(h.scroll, 0, h.maxScroll())
	return h, nil
}

// helpColTarget 是单列的目标宽度（含列间隔），用于按终端宽度决定列数：
// 内容区每 ~34 列放一列，窄终端自然退化为单列。helpColDividerW 是列间隔
// 线 " │ " 的显示宽度；helpMarginX/Y 是浮窗相对外框内容区的留白（左右各
// 1 列、上下各 1 行），让底层 Tab 内容在四周露出。
const (
	helpColTarget   = 34
	helpColDividerW = 3
	helpMarginX     = 2
	helpMarginY     = 2
)

// helpCell 是布局网格里的一个单元：一段纯文本加渲染样式。indent 是续行
// 缩进（显示列数）——键位单元把续行对齐到描述列，超宽条目按词折行而不是
// 中途截断。
type helpCell struct {
	text   string
	style  lipgloss.Style
	indent int
}

func (h *helpTab) View() string {
	rows, footer := h.layout()
	visible := h.visibleRows()
	start := clamp(h.scroll, 0, h.maxScroll())
	end := start + visible
	if end > len(rows) {
		end = len(rows)
	}
	body := append(rows[start:end], footer)
	box := searchOverlayStyle.Render(strings.Join(body, "\n"))
	// 底栏恒占一行（见 visibleRows 的 -1），正常情况恰好不触发截断；这里
	// 的 clipLines 只是超小终端下的兜底。
	return clipLines(box, maxInt(h.height-frameRows-helpMarginY, 1))
}

// visibleRows 是浮窗框内的键位行数：框总高上限（外框 5 行 + 上下留白）
// 减去边框/内边距，再恒减 1 行底栏。
func (h *helpTab) visibleRows() int {
	v := h.height - frameRows - helpMarginY - overlayRows - 1
	return maxInt(v, 1)
}

// maxScroll 是滚动偏移上限：布局行数超出可视行数的部分。
func (h *helpTab) maxScroll() int {
	rows, _ := h.layout()
	return maxInt(len(rows)-h.visibleRows(), 0)
}

// layout 生成全部内容行（最多两列拼好的整行）与底栏。View 与 Update
// 共用同一份计算，保证滚动窗口与实际渲染一致。
func (h *helpTab) layout() (rows []string, footer string) {
	// 框内文本区：外框 2 列 + 浮窗左右留白 + overlay 边框/内边距 6 列。
	contentW := maxInt(h.width-overlayCols-2-helpMarginX, 10)
	items := h.items()

	// 高度优先：先按单列布局，行数放得下就保持一列；放不下再均分两列。
	// 至多两列——宽终端不会碎成更多列；宽度只决定列宽下限（单列不足 14
	// 列宽即终端极窄时退化为单列，溢出部分走滚动）。
	maxCols := minInt((contentW+helpColDividerW)/helpColTarget, 2)
	if maxCols < 1 {
		maxCols = 1
	}
	if maxCols > len(items) {
		maxCols = len(items)
	}
	visible := h.visibleRows()
	for c := 1; c <= maxCols; c++ {
		colW := contentW
		if c > 1 {
			colW = (contentW - (c-1)*helpColDividerW) / c
		}
		if c > 1 && colW < 14 {
			break
		}
		rows = gridRows(items, c, maxInt(colW, 8))
		if len(rows) <= visible {
			break
		}
	}
	// statusBarStyle 自带 Padding(0,1)：footer 截断到内容区宽 -2。
	foot := "? / esc close"
	if len(rows) > h.visibleRows() {
		foot = "? / esc close · ↑↓/jk/pgup/pgdn scroll"
	}
	footer = statusBarStyle.Render(truncateWidth(foot, contentW-2))
	return rows, footer
}

// items 把 global 与 tab 专属键位整理成单元列表（纯文本；超宽条目由
// gridRows 按词折行，续行缩进对齐到描述列）。分类标题（Global、Tab 名、
// 分组名）用 accent 色加粗，与键位行区分。
func (h *helpTab) items() []helpCell {
	heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorAccent))
	kw := h.keyWidth()
	blank := helpCell{"", lipgloss.NewStyle(), 0}
	items := []helpCell{{"Global", heading, 0}}
	for _, b := range globalKeys {
		items = append(items, bindingCell(b, kw))
	}
	if h.title != "" {
		items = append(items, blank, helpCell{h.title, heading, 1})
		if len(h.bindings) == 0 {
			items = append(items, helpCell{"  (no tab-specific keys)", lipgloss.NewStyle(), 2})
		}
		// Group headings on first appearance, so sections stay contiguous
		// even when a tab interleaves groups in Bindings().
		seen := make(map[string]bool)
		for _, b := range h.bindings {
			g := b.Group
			if g == "" {
				g = "Keys"
			}
			if !seen[g] {
				seen[g] = true
				items = append(items, helpCell{" " + g, heading, 1})
			}
			items = append(items, bindingCell(b, kw))
		}
	}
	return items
}

// keyWidth 是键名列宽：取全部键位显示宽度的最大值，夹在 [6,15]。
func (h *helpTab) keyWidth() int {
	w := 6
	for _, b := range globalKeys {
		if x := lipgloss.Width(strings.Join(b.Keys, "/")); x > w {
			w = x
		}
	}
	for _, b := range h.bindings {
		if x := lipgloss.Width(strings.Join(b.Keys, "/")); x > w {
			w = x
		}
	}
	return minInt(w, 15)
}

// bindingCell 渲染一条键位为单元：键名左对齐占 kw 列，描述随后；续行
// 缩进到 kw+3 列对齐描述起始。
func bindingCell(b KeyAction, kw int) helpCell {
	return helpCell{fmt.Sprintf("  %-*s %s", kw, strings.Join(b.Keys, "/"), b.Desc), lipgloss.NewStyle(), kw + 3}
}

// gridRows 把单元按列优先填入 cols 列网格（先填满一列再开下一列），列高
// 按总可视行数均分——调用方按"高度不够再加列"的策略选定 cols。单元右补齐
// 到列宽，列间用 muted 竖线隔开；超宽单元按词折行，每个可视行单独占
// rows 的一项，滚动按可视行计数。
func gridRows(items []helpCell, cols, colW int) []string {
	cells := make([][]string, len(items))
	total := 0
	for i, it := range items {
		cells[i] = cellLines(it, colW)
		total += len(cells[i])
	}
	// 每列的目标行数：总行数均分。当前列装不下下一个单元时开新列，但只
	// 在还没开满 cols 列时——超限的大单元连同剩余都收进最后一列，保证物
	// 理列数不超过 cols（调用方按"最多两列"依赖这个上限）。
	target := (total + cols - 1) / cols
	var columns [][]string
	var cur []string
	curLines := 0
	for i := range cells {
		if len(cur) > 0 && curLines+len(cells[i]) > target && len(columns) < cols-1 {
			columns = append(columns, cur)
			cur, curLines = nil, 0
		}
		cur = append(cur, cells[i]...)
		curLines += len(cells[i])
	}
	columns = append(columns, cur)

	gutter := " " + mutedStyle().Render("│") + " "
	height := 0
	for _, col := range columns {
		height = maxInt(height, len(col))
	}
	var rows []string
	for r := 0; r < height; r++ {
		parts := make([]string, 0, len(columns))
		for _, col := range columns {
			if r < len(col) {
				parts = append(parts, col[r])
			} else {
				parts = append(parts, strings.Repeat(" ", colW))
			}
		}
		rows = append(rows, strings.Join(parts, gutter))
	}
	return rows
}

// cellLines 渲染一个单元为补齐到列宽的行列表：超宽单元按词折行（续行
// 缩进到 indent 列对齐描述起始）。
func cellLines(it helpCell, colW int) []string {
	if lipgloss.Width(it.text) <= colW {
		return []string{it.style.Render(padRight(it.text, colW))}
	}
	var out []string
	for _, l := range wrapCell(it.text, colW, it.indent) {
		out = append(out, it.style.Render(l))
	}
	return out
}

// wrapCell 把超宽单元按词折行到 colW：首行保留键名列（indent 列），续行
// 缩进到 indent 列让描述对齐；超长单词按宽硬切（键位描述均为 ASCII）。
func wrapCell(text string, colW, indent int) []string {
	rs := []rune(text)
	if len(rs) <= indent {
		return []string{padRight(text, colW)}
	}
	width := colW - indent
	if width < 4 {
		width = 4
	}
	chunks := wrapWords(string(rs[indent:]), width)
	lines := make([]string, 0, len(chunks))
	lines = append(lines, padRight(string(rs[:indent])+chunks[0], colW))
	for _, c := range chunks[1:] {
		lines = append(lines, padRight(strings.Repeat(" ", indent)+c, colW))
	}
	return lines
}

// wrapWords 按空格贪心折行到 width；仍超宽的单个单词按 width 硬切。
func wrapWords(s string, width int) []string {
	var wrapped []string
	cur := ""
	flush := func() {
		rs := []rune(cur)
		for len(rs) > width {
			wrapped = append(wrapped, string(rs[:width]))
			rs = rs[width:]
		}
		if len(rs) > 0 {
			wrapped = append(wrapped, string(rs))
		}
		cur = ""
	}
	for _, w := range strings.Fields(s) {
		if cur == "" {
			cur = w
			continue
		}
		if len(cur)+1+len(w) <= width {
			cur += " " + w
			continue
		}
		flush()
		cur = w
	}
	flush()
	return wrapped
}

// padRight 把 s 用空格右补齐到 n 个显示列；超过则原样返回（调用方已截断）。
func padRight(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// Compile-time guard.
var _ Tab = (*helpTab)(nil)
