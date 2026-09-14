package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// openHelp 构造一个已打开 help overlay 的 model（尺寸为完整终端大小）。
func openHelp(t *testing.T, mgrs Managers, w, h int) Model {
	t.Helper()
	m := New(mgrs)
	out, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = out.(Model)
	out, _ = m.Update(runeKey("?"))
	m = out.(Model)
	if m.help == nil {
		t.Fatal("? should open the help overlay")
	}
	return m
}

// stripANSI 去掉 ANSI 转义，便于断言布局内容。
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// TestHelpOverlayFitsScreen 回归：help 框体必须恰好落在外框内容区内。
// 行宽不截断时（旧实现），长描述会把框体顶出外框、边框折行撑爆整屏。
func TestHelpOverlayFitsScreen(t *testing.T) {
	mgrs := Managers{
		History: &fakeHistorySource{rows: sampleHistoryRows()},
		Audit:   &fakeAuditSource{rows: sampleAuditRows()},
	}
	for _, size := range [][2]int{{80, 24}, {50, 20}, {100, 30}, {34, 10}} {
		m := openHelp(t, mgrs, size[0], size[1])
		v := m.View()
		if w, h := lipgloss.Width(v), lipgloss.Height(v); w != size[0] || h != size[1] {
			t.Errorf("size=%dx%d: help view=%dx%d, want exactly %dx%d\n%s",
				size[0], size[1], w, h, size[0], size[1], v)
		}
	}
}

// TestHelpOverlayMultiColumn 宽终端按两列排布键位（高度优先：单列放不下才
// 分列），列与列之间有竖线分隔；窄终端退化为单列。
func TestHelpOverlayMultiColumn(t *testing.T) {
	mgrs := Managers{}

	// 两列布局的内容行里，列间隔线让整行的 "│" 数达到 5（外框 2 + 浮窗
	// 2 + 间隔 1）；单列只有 4。列优先填充下同列相邻键位不占同一行，不能
	// 再用"同行出现两个键位"判别。
	twoColRows := func(w, h int) []string {
		return strings.Split(stripANSI(openHelp(t, mgrs, w, h).View()), "\n")
	}
	for _, size := range [][2]int{{100, 24}, {80, 24}, {160, 40} /* 再宽也不许超过两列 */} {
		twoCol := false
		for _, row := range twoColRows(size[0], size[1]) {
			if strings.Contains(row, "cycle tabs") && strings.Count(row, "│") >= 5 {
				twoCol = true
			}
		}
		if !twoCol {
			t.Errorf("%d-wide help should lay bindings out in two columns, got:\n%s",
				size[0], openHelp(t, mgrs, size[0], size[1]).View())
		}
	}

	for _, row := range twoColRows(50, 20) {
		// 单列时一行只有外框与浮窗自身的左右边框（4 条竖线），不应出现列间隔线。
		if strings.Contains(row, "cycle tabs") && strings.Count(row, "│") != 4 {
			t.Errorf("single-column help should not show a column divider: %q", row)
		}
	}
}

// TestHelpOverlayFloats 回归：`?` 总览是浮窗：叠放在当前 Tab 内容之上
// （底层内容在四周露出），且框体底边框不被截断。
func TestHelpOverlayFloats(t *testing.T) {
	mgrs := Managers{}
	m := openHelp(t, mgrs, 100, 30)

	rows := strings.Split(stripANSI(m.View()), "\n")
	topIdx, botIdx := -1, -1
	for i, r := range rows {
		if strings.HasPrefix(r, "│ ╭") && topIdx == -1 {
			topIdx = i
		}
		if strings.HasPrefix(r, "│ ╰") {
			botIdx = i
		}
	}
	if topIdx < 3 {
		t.Errorf("help box should float below the tab strip, top at row %d\n%s", topIdx, m.View())
	}
	if botIdx == -1 || botIdx >= len(rows)-2 {
		t.Errorf("help box bottom border should be visible above the status bar, bottom at row %d of %d\n%s", botIdx, len(rows), m.View())
	}
	// 浮窗上沿之上应露出底层 Tab 内容（ pane 边框线），而不是空白填充。
	if topIdx > 0 && !strings.Contains(rows[topIdx-1], "─") {
		t.Errorf("row above the floating help should show the underlying tab content, got %q", rows[topIdx-1])
	}
}

// TestHelpOverlayScrolls 内容超过框体高度时可以滚动，且滚动有上下限。
func TestHelpOverlayScrolls(t *testing.T) {
	mgrs := Managers{}
	m := openHelp(t, mgrs, 56, 14) // 可视 2 行，global 键位即超出

	first := m.View()
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = out.(Model)
	if m.View() == first {
		t.Error("down should scroll the help content")
	}
	top := m.help.scroll
	for i := 0; i < 50; i++ {
		out, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = out.(Model)
	}
	if m.help.scroll <= top {
		t.Errorf("scroll should advance, got %d", m.help.scroll)
	}
	maxAfterDown := m.help.scroll
	// 继续按 down 不得越过上限
	if m.help.scroll != m.help.maxScroll() {
		t.Errorf("scroll = %d, want clamped at maxScroll %d", m.help.scroll, m.help.maxScroll())
	}
	_ = maxAfterDown
	out, _ = m.Update(runeKey("g"))
	m = out.(Model)
	if m.help.scroll != 0 {
		t.Errorf("g should jump to top, scroll = %d", m.help.scroll)
	}
	// G 跳到底
	out, _ = m.Update(runeKey("G"))
	m = out.(Model)
	if m.help.scroll != m.help.maxScroll() {
		t.Errorf("G should jump to bottom, scroll = %d want %d", m.help.scroll, m.help.maxScroll())
	}
	// 可滚动时底栏给出滚动提示
	if !strings.Contains(stripANSI(m.View()), "scroll") {
		t.Error("scrollable help footer should mention scroll keys")
	}
}

// TestHelpOverlayWrapsLongDescriptions 长描述按词折行到列内，续行缩进对齐
// 描述列，不得中途截断出省略号；折行后每个可视行宽度仍不超列预算。
func TestHelpOverlayWrapsLongDescriptions(t *testing.T) {
	items := []helpCell{
		{"Global", lipgloss.NewStyle().Bold(true), 0},
		bindingCell(KeyAction{Keys: []string{"esc"}, Desc: "back: clear filter / close overlay / wizard back"}, 13),
		bindingCell(KeyAction{Keys: []string{"q"}, Desc: "quit"}, 13),
	}
	rows := gridRows(items, 2, 33)
	joined := stripANSI(strings.Join(rows, "\n"))
	if !strings.Contains(joined, "back: clear") || !strings.Contains(joined, "overlay / wizard") ||
		!strings.Contains(joined, strings.Repeat(" ", 16)+"back") {
		t.Errorf("long description should wrap fully, got:\n%s", joined)
	}
	if strings.Contains(joined, "…") {
		t.Errorf("wrapped rows should not truncate with ellipsis:\n%s", joined)
	}
	for _, r := range rows {
		if w := lipgloss.Width(stripANSI(r)); w > 2*33+helpColDividerW {
			t.Errorf("row wider than column budget: %d > %d: %q", w, 2*33+helpColDividerW, r)
		}
	}
	// 续行缩进对齐描述列（kw+3 = 16 列）。
	for _, r := range rows {
		plain := stripANSI(r)
		if strings.Contains(plain, "wizard back") && !strings.HasPrefix(plain, strings.Repeat(" ", 16)) {
			t.Errorf("continuation line should align under the description column: %q", plain)
		}
	}
}

// TestSearchOverlayFitsScreen 回归：search 框体宽度预算必须包含外框 2 列
// 边框与 emptyState 内边距，否则输入内容一宽就在外框内折行。
func TestSearchOverlayFitsScreen(t *testing.T) {
	mgrs := Managers{
		History: &fakeHistorySource{rows: sampleHistoryRows()},
		Audit:   &fakeAuditSource{rows: sampleAuditRows()},
	}
	for _, size := range [][2]int{{80, 24}, {50, 20}, {100, 30}} {
		m := New(mgrs)
		out, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = out.(Model)
		out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
		m = out.(Model)
		if m.search == nil {
			t.Fatalf("size=%v: S should open the search overlay", size)
		}
		// 输入长串，让回显行逼近宽度预算（旧实现此时折行）
		for _, ch := range "deploy-key-needle" {
			out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
			m = out.(Model)
		}
		v := m.View()
		if w, h := lipgloss.Width(v), lipgloss.Height(v); w != size[0] || h != size[1] {
			t.Errorf("size=%dx%d: search view=%dx%d, want exactly %dx%d\n%s",
				size[0], size[1], w, h, size[0], size[1], v)
		}
	}
}

// TestHelpOverlayNarrowStillShowsBindings 回归（review 2026-09-13/ssh-sync P1）：
// contentW ≤ 32 的窄终端（≥30 宽是 TUI 最小尺寸）曾因 maxCols 归零渲染出
// 空内容体（只剩底栏）。单列保底后至少应看到「Global」分组头；高度足够
// 时首个键位行也直接可见。
func TestHelpOverlayNarrowStillShowsBindings(t *testing.T) {
	mgrs := Managers{}
	for _, size := range [][2]int{{34, 10}, {36, 14}, {40, 24}} {
		v := stripANSI(openHelp(t, mgrs, size[0], size[1]).View())
		if !strings.Contains(v, "Global") {
			t.Errorf("size=%dx%d: help body is blank at narrow width:\n%s", size[0], size[1], v)
		}
	}
	v := stripANSI(openHelp(t, mgrs, 40, 24).View())
	if !strings.Contains(v, "close") {
		t.Errorf("40x24: help body missing binding rows:\n%s", v)
	}
}
