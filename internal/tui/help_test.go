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

// TestHelpOverlayMultiColumn 宽终端按多列排布键位（密度低的内容不占满整行）；
// 窄终端退化为单列。
func TestHelpOverlayMultiColumn(t *testing.T) {
	mgrs := Managers{}

	wide := openHelp(t, mgrs, 80, 24)
	wideRows := strings.Split(stripANSI(wide.View()), "\n")
	twoCol := false
	for _, row := range wideRows {
		if strings.Contains(row, "cycle tabs") && strings.Contains(row, "global search") {
			twoCol = true
		}
	}
	if !twoCol {
		t.Errorf("80-wide help should lay bindings out in two columns, got:\n%s", wide.View())
	}

	narrow := openHelp(t, mgrs, 50, 20)
	narrowRows := strings.Split(stripANSI(narrow.View()), "\n")
	for _, row := range narrowRows {
		if strings.Contains(row, "cycle tabs") && strings.Contains(row, "global search") {
			t.Errorf("50-wide help should be single column, but two bindings share a row: %q", row)
		}
	}
}

// TestHelpOverlayScrolls 内容超过框体高度时可以滚动，且滚动有上下限。
func TestHelpOverlayScrolls(t *testing.T) {
	mgrs := Managers{}
	m := openHelp(t, mgrs, 56, 14) // 可视约 5 行，global 键位即超出

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
