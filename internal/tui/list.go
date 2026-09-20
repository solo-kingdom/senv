package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// paneBudget is the rendered height of a two-pane tab: inner height plus the
// 2-row rounded border lipgloss draws outside Height().
func paneBudget(innerH int) int { return innerH + 2 }

// stackWithOverlay renders the two-pane body at a reduced height so a bottom
// prompt/flash stays inside the pane budget instead of pushing the list off-screen.
func stackWithOverlay(innerH int, overlay string, renderBody func(h int) string) string {
	if overlay == "" {
		return renderBody(innerH)
	}
	oh := lipgloss.Height(overlay)
	bodyH := innerH - oh
	if bodyH < 1 {
		bodyH = 1
	}
	out := lipgloss.JoinVertical(lipgloss.Left, renderBody(bodyH), overlay)
	return clipLines(out, paneBudget(innerH))
}

// cursorPrefix reserves two columns on every list row so selected ("▸ ") and
// unselected ("  ") lines share the same column start.
func cursorPrefix(selected bool) string {
	if selected {
		return "▸ "
	}
	return "  "
}

// padRunes pads or truncates s to exactly n runes.
func padRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) > n {
		return truncRunes(s, n)
	}
	return s + strings.Repeat(" ", n-len(r))
}

// listPageSize is the number of list rows that fit under a 1-line pane title.
// A non-positive height means "no window" (show everything).
func listPageSize(height int) int {
	if height <= 1 {
		return 0
	}
	return height - 1
}

// visibleRange returns a half-open [start, end) window of n items that includes
// cursor and is at most page rows. page <= 0 shows the whole list.
func visibleRange(n, cursor, page int) (start, end int) {
	if n <= 0 {
		return 0, 0
	}
	if page <= 0 || n <= page {
		return 0, n
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= n {
		cursor = n - 1
	}
	start = cursor - page + 1
	if start < 0 {
		start = 0
	}
	if start+page > n {
		start = n - page
	}
	return start, start + page
}

// clipLines keeps at most n lines of s (no-op when n <= 0). Used as a last
// line of defence so a pane cannot push the rest of the TUI off-screen.
func clipLines(s string, n int) string {
	if n <= 0 || s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n")
}

// truncateWidth truncates s to at most maxCols display columns, appending "…".
// Unlike truncateRunes it accounts for double-width (CJK) runes, so a line can
// never be wider on screen than the pane it is rendered into.
func truncateWidth(s string, maxCols int) string {
	if maxCols <= 0 || lipgloss.Width(s) <= maxCols {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := lipgloss.Width(string(r))
		if used+w > maxCols-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String() + "…"
}

// windowedPane renders a titled list that stays within height rows, scrolling
// so that cursor remains visible. When the list is longer than the pane, the
// title shows the visible 1-based range (e.g. "Groups (12)  4–12"). width is
// the pane Width; the title is truncated so lipgloss Width-wrap cannot add
// extra rows (title style padding plus pane padding consume 4 columns).
func windowedPane(title string, lines []string, cursor, height, width int) string {
	page := listPageSize(height)
	start, end := visibleRange(len(lines), cursor, page)
	if start > 0 || end < len(lines) {
		title = fmt.Sprintf("%s  %d–%d", title, start+1, end)
	}
	if width > 4 {
		title = truncateWidth(title, width-4)
	}
	parts := make([]string, 0, 1+end-start)
	parts = append(parts, paneTitleStyle.Render(title))
	if end > start {
		parts = append(parts, lines[start:end]...)
	}
	out := lipgloss.JoinVertical(lipgloss.Left, parts...)
	if height > 0 {
		out = clipLines(out, height)
	}
	return out
}

// ---------- 共享列表组件（tui-ux-list） ----------

// List 是共享列表视图状态：游标 + 可视高度。它不持有业务数据，也不做行
// 渲染——Tab 负责把数据变成行，组件负责「可见哪些行、游标在哪」。过滤与
// 多选状态由后续子 change 在其上叠加。
type List struct {
	cursor   int
	height   int // 可视行预算；<=0 表示不窗口化（全量展示）
	selected map[string]bool
}

// Cursor 返回当前游标。
func (l *List) Cursor() int { return l.cursor }

// SetHeight 更新可视行预算。
func (l *List) SetHeight(h int) { l.height = h }

// SetCursor 将游标 clamp 到 [0, n)。
func (l *List) SetCursor(idx, n int) {
	if n <= 0 {
		l.cursor = 0
		return
	}
	l.cursor = clamp(idx, 0, n-1)
}

// Move 游标移动 delta（clamp 到列表范围）。
func (l *List) Move(delta, n int) { l.SetCursor(l.cursor+delta, n) }

// Home/End 跳顶/跳底。
func (l *List) Home() { l.cursor = 0 }
func (l *List) End(n int) {
	if n > 0 {
		l.cursor = n - 1
	}
}

// Page 按当前可视页整页移动游标（dir=-1 上翻，1 下翻）。无窗口化（height
// 预算 <=0，全量展示）时整页 = 整个列表：直接跳顶/跳底。
func (l *List) Page(dir, n int) {
	page := listPageSize(l.height)
	if page <= 0 {
		if dir < 0 {
			l.Home()
		} else {
			l.End(n)
		}
		return
	}
	l.Move(dir*page, n)
}

// VisibleRange 返回包含游标的可见窗口 [start, end)。
func (l *List) VisibleRange(n int) (start, end int) {
	return visibleRange(n, l.cursor, listPageSize(l.height))
}

// paneBudgets 计算双栏宽度：左栏 = width*ratioNum/ratioDen 并 clamp 到
// [minLeft, maxLeft]，右栏吃剩余（预留 5 列栏间 chrome：1 间隙 + 左右栏
// 各 2 列边框，lipgloss 边框画在 Width 之外）。
func paneBudgets(width, ratioNum, ratioDen, minLeft, maxLeft int) (left, right int) {
	left = width * ratioNum / ratioDen
	if left > maxLeft {
		left = maxLeft
	}
	if left < minLeft {
		left = minLeft
	}
	// 小宽度兜底：minLeft clamp 可能把左栏顶过 width-9，右栏挤压后总宽
	// 超出可用宽度。两栏各保留 4 列下限，总宽 MUST NOT 超过 width。
	if cap := width - 9; left > cap {
		left = maxInt(cap, 4)
	}
	right = width - left - 5
	if right < 4 {
		right = 4
	}
	return left, right
}

// ---------- 多选集（tui-ux-multiselect） ----------

// Toggle 勾选/取消勾选一个稳定标识（如 "group/key"、alias）。
func (l *List) Toggle(key string) {
	if l.selected == nil {
		l.selected = map[string]bool{}
	}
	if l.selected[key] {
		delete(l.selected, key)
		return
	}
	l.selected[key] = true
}

// SelectVisible 全选/取消全选可见集：可见集已全部选中则整体取消，否则整体勾选。
// 空可见集是显式 no-op（不改变既有选择）。
func (l *List) SelectVisible(keys []string) {
	if len(keys) == 0 {
		return
	}
	if l.selected == nil {
		l.selected = map[string]bool{}
	}
	allSelected := true
	for _, k := range keys {
		if !l.selected[k] {
			allSelected = false
			break
		}
	}
	for _, k := range keys {
		if allSelected {
			delete(l.selected, k)
		} else {
			l.selected[k] = true
		}
	}
}

// IsSelected 报告标识是否在多选集中。
func (l *List) IsSelected(key string) bool { return l.selected[key] }

// SelectionCount 返回多选集大小（含被过滤隐藏的已选项）。
func (l *List) SelectionCount() int { return len(l.selected) }

// Selected 返回全部已选标识（字典序）；宿主据此做选择集与数据的 reconcile。
func (l *List) Selected() []string {
	out := make([]string, 0, len(l.selected))
	for k := range l.selected {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SelectedIn 返回 keys 中被选中的个数（宿主据此计算「被过滤隐藏」数）。
func (l *List) SelectedIn(keys []string) int {
	n := 0
	for _, k := range keys {
		if l.selected[k] {
			n++
		}
	}
	return n
}

// ClearSelection 清空多选集（批量操作提交后调用）。
func (l *List) ClearSelection() { l.selected = nil }

// SelectionHint 渲染标题栏计数提示；无勾选返回空串。hidden 为被过滤隐藏
// 的已选数（>0 时附带提示）。
func (l *List) SelectionHint(hidden int) string {
	n := len(l.selected)
	if n == 0 {
		return ""
	}
	if hidden > 0 {
		return fmt.Sprintf(" · selected %d (%d filtered)", n, hidden)
	}
	return fmt.Sprintf(" · selected %d", n)
}

// ---------- 分组侧栏（tui-ux-sidebar：config 范式下沉共享） ----------

// SidebarRow 是侧栏一行的展示数据。
type SidebarRow struct {
	Marker   string // 行首标记（"◯"=All、"●"=激活、" "=普通）
	Name     string
	Count    int
	Selected bool
}

// renderSidebar 渲染分组侧栏（config 范式：All 置顶 + 过滤感知计数），
// 三处（config/env/text）共用同一实现以保证视觉与行为一致。
func renderSidebar(rows []SidebarRow, cursor, height, width int) string {
	inner := width - 2
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		line := truncateRunes(fmt.Sprintf("%s %s  [%d]", r.Marker, r.Name, r.Count), inner-2)
		if r.Selected {
			line = selectedLineStyle.Render(cursorPrefix(true) + line)
		} else {
			line = cursorPrefix(false) + line
		}
		lines = append(lines, line)
	}
	return windowedPane(fmt.Sprintf("Groups (%d)", len(rows)), lines, cursor, height, width)
}
