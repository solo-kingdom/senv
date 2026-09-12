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
// It renders in place of the tab content, like the search overlay: while open
// it captures every key, and `?`/`esc` close it. Rows are truncated to the
// overlay's inner width, laid out in multiple columns when the terminal is
// wide enough (binding lists are sparse, one-per-line wastes most of the
// screen), and scrollable when the laid-out rows still exceed the box height.
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

// helpColTarget 是单列的目标宽度（含间隔），用于按终端宽度决定列数：
// 内容区每 ~37 列放一列，窄终端自然退化为单列。
const (
	helpColTarget = 37
	helpColGutter = 2
)

// helpCell 是布局网格里的一个单元：一段纯文本加渲染样式。text 在入格前
// 已按目标宽度截断，因此样式化后显示宽度可控。
type helpCell struct {
	text  string
	style lipgloss.Style
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
	return clipLines(box, h.height-frameRows)
}

// visibleRows 是 overlay 框内的内容行数（框总高上限减边框/内边距）。
func (h *helpTab) visibleRows() int {
	v := h.height - frameRows - overlayRows
	return maxInt(v, 1)
}

// maxScroll 是滚动偏移上限：布局行数超出可视行数的部分。
func (h *helpTab) maxScroll() int {
	rows, _ := h.layout()
	return maxInt(len(rows)-h.visibleRows(), 0)
}

// layout 生成全部内容行（多列网格拼好的整行）与底栏。View 与 Update
// 共用同一份计算，保证滚动窗口与实际渲染一致。
func (h *helpTab) layout() (rows []string, footer string) {
	// 框内文本区：外框 2 列 + overlay 边框/内边距 6 列。
	contentW := maxInt(h.width-overlayCols-2, 10)
	items := h.items()

	// 列数：内容区能放下几列 37 列宽的格子；剩余宽度均摊到各列。
	cols := (contentW + helpColGutter) / helpColTarget
	if cols < 1 {
		cols = 1
	}
	colW := (contentW - (cols-1)*helpColGutter) / cols
	if colW < 14 {
		cols = 1
		colW = contentW
	}
	colW = maxInt(colW, 8)

	rows = gridRows(items, cols, colW, contentW)
	// statusBarStyle 自带 Padding(0,1)：footer 截断到内容区宽 -2。
	foot := "? / esc close"
	if len(rows) > h.visibleRows() {
		foot = "? / esc close · ↑↓/jk/pgup/pgdn scroll"
	}
	footer = statusBarStyle.Render(truncateWidth(foot, contentW-2))
	return rows, footer
}

// items 把 global 与 tab 专属键位整理成单元列表（纯文本，未按列截断；
// 超长行在 gridRows 里截断并可能独占整行）。
func (h *helpTab) items() []helpCell {
	heading := lipgloss.NewStyle().Bold(true)
	kw := h.keyWidth()
	blank := helpCell{"", lipgloss.NewStyle()}
	items := []helpCell{{"Global", heading}}
	for _, b := range globalKeys {
		items = append(items, bindingCell(b, kw))
	}
	if h.title != "" {
		items = append(items, blank, helpCell{h.title, heading})
		if len(h.bindings) == 0 {
			items = append(items, helpCell{"  (no tab-specific keys)", lipgloss.NewStyle()})
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
				items = append(items, helpCell{" " + g, heading})
			}
			items = append(items, bindingCell(b, kw))
		}
	}
	return items
}

// keyWidth 是键名列宽：取全部键位显示宽度的最大值，夹在 [6,14]。
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
	return minInt(w, 14)
}

// bindingCell 渲染一条键位为单元：键名左对齐占 kw 列，描述随后。
func bindingCell(b KeyAction, kw int) helpCell {
	return helpCell{fmt.Sprintf("  %-*s %s", kw, strings.Join(b.Keys, "/"), b.Desc), lipgloss.NewStyle()}
}

// gridRows 把单元按行优先填入 cols 列网格：普通单元占一格并左对齐补齐到
// 列宽；超过列宽的单元独占整行（截断到内容区宽），避免长描述被硬折行。
func gridRows(items []helpCell, cols, colW, contentW int) []string {
	gutter := strings.Repeat(" ", helpColGutter)
	var rows []string
	var cells []string
	flush := func() {
		if len(cells) == 0 {
			return
		}
		rows = append(rows, strings.Join(cells, gutter))
		cells = cells[:0]
	}
	for _, it := range items {
		if lipgloss.Width(it.text) > colW {
			flush()
			rows = append(rows, truncateWidth(it.text, contentW))
			continue
		}
		cells = append(cells, it.style.Render(padRight(it.text, colW)))
		if len(cells) == cols {
			flush()
		}
	}
	flush()
	return rows
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
