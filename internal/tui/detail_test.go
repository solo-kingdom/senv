package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// detailTestLines 造 n 行可辨认的内容，用于验证滚动窗口。
func detailTestLines(n int) []string {
	lines := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		lines = append(lines, fmt.Sprintf("row-%02d", i))
	}
	return lines
}

// TestDetailOverlayFillsPaneSlot 校验弹层占用与面板完全相同的位置：终端尺寸
// 78×17 的内容区对应 78×19 的面板槽，弹层也必须正好是 78×19，且不随滚动位置
// 或内容多寡变形（曾经按内容自适应高度，滚动时下边缘会跟着动）。
func TestDetailOverlayFillsPaneSlot(t *testing.T) {
	const wantW, wantH = 78, 19

	long := newDetailOverlay("KeyPair web-key", detailTestLines(40))
	long.SetSize(78, 17)
	for _, top := range []int{0, 1, 5, 20, long.maxTop(), 999} {
		long.top = top
		out := long.View()
		if w, h := lipgloss.Width(out), lipgloss.Height(out); w != wantW || h != wantH {
			t.Fatalf("top=%d: overlay = %dx%d, want %dx%d", top, w, h, wantW, wantH)
		}
	}

	// 内容不足一页同样撑满整个面板槽。
	short := newDetailOverlay("KeyPair tiny", []string{"a", "b"})
	short.SetSize(78, 17)
	if out := short.View(); lipgloss.Width(out) != wantW || lipgloss.Height(out) != wantH {
		t.Fatalf("short content overlay = %dx%d, want %dx%d",
			lipgloss.Width(out), lipgloss.Height(out), wantW, wantH)
	}
}

// TestDetailOverlayScrollWindow 校验滚动窗口：一页显示 d.height-6 行，滚动到
// 底时最后一行落在框底（不出现「一行 + 空白尾巴」），pgup/pgdn 整页移动。
func TestDetailOverlayScrollWindow(t *testing.T) {
	d := newDetailOverlay("t", detailTestLines(40))
	d.SetSize(78, 17)
	page := d.pageSize()
	if page != 11 {
		t.Fatalf("pageSize = %d, want 11", page)
	}

	out := d.View()
	if !strings.Contains(out, "row-01") || strings.Contains(out, "row-12") {
		t.Fatalf("first page must hold exactly %d rows:\n%s", page, out)
	}
	if !strings.Contains(out, "1–11/40") {
		t.Fatalf("title range missing:\n%s", firstLine(out))
	}

	// 滚到底：最后一行可见，且窗口停在最后一个整页。
	for i := 0; i < 100; i++ {
		d, _ = d.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if d.top != d.maxTop() {
		t.Fatalf("top = %d, want maxTop %d", d.top, d.maxTop())
	}
	out = d.View()
	if !strings.Contains(out, "row-40") {
		t.Fatalf("last row must be visible at the bottom:\n%s", out)
	}
	if !strings.Contains(out, "30–40/40") {
		t.Fatalf("tail range missing:\n%s", firstLine(out))
	}
}

// firstLine 返回首行，便于把标题范围的断言失败信息压到一行。
func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	return s
}
