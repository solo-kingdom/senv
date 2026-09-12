package tui

// tui-review-fixes-hygiene 回归用例：search/list/config 内部缺陷逐条锁定。
// ai 重复条件与 env 死代码为编译期证明（无行为差异），不设用例。

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TestSearchFollowsResize：overlay 打开期间 WindowSizeMsg 跟随更新尺寸
// （审查 search.go:194）。
func TestSearchFollowsResize(t *testing.T) {
	s := newSearchTab(Managers{})
	s.SetSize(80, 24)
	out, _ := s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	st := out.(*searchTab)
	if st.width != 120 || st.height != 40 {
		t.Fatalf("search size after resize = %dx%d, want 120x40", st.width, st.height)
	}
}

// TestSearchHeaderTruncates：长输入的头部不得超出 overlay 宽度
// （审查 search.go:289）。
func TestSearchHeaderTruncates(t *testing.T) {
	s := newSearchTab(Managers{})
	s.SetSize(60, 24)
	s.input = strings.Repeat("w", 200)
	out := s.View()
	for i, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w > 60 {
			t.Fatalf("row %d width %d exceeds 60: %q", i, w, ln)
		}
	}
}

// TestSearchBindingsNoUnreachableKey：不可达的 "type" 键位声明已移除
// （审查 search.go:62）。
func TestSearchBindingsNoUnreachableKey(t *testing.T) {
	s := newSearchTab(Managers{})
	for _, b := range s.Bindings() {
		for _, k := range b.Keys {
			if k == "type" {
				t.Fatal("unreachable key binding 'type' still declared")
			}
		}
	}
}

// TestPaneBudgetsFitsWidth：小宽度下双栏加 chrome 总宽不得超出可用宽度
// （审查 list.go:207）。
func TestPaneBudgetsFitsWidth(t *testing.T) {
	for width := 13; width <= 60; width++ {
		left, right := paneBudgets(width, 2, 5, 24, 26)
		if left+right+5 > width {
			t.Fatalf("width %d: left=%d right=%d total=%d exceeds width", width, left, right, left+right+5)
		}
		if left < 4 || right < 4 {
			t.Fatalf("width %d: pane below 4-col floor (left=%d right=%d)", width, left, right)
		}
	}
}

// TestListPageWithoutWindowJumpsToEnd：无窗口化时整页翻动 = 整列表
// （审查 list.go:193）。
func TestListPageWithoutWindowJumpsToEnd(t *testing.T) {
	var l List
	l.SetHeight(0) // 不窗口化
	l.Page(1, 5)
	if l.Cursor() != 4 {
		t.Fatalf("page down without window: cursor = %d, want 4 (end)", l.Cursor())
	}
	l.Page(-1, 5)
	if l.Cursor() != 0 {
		t.Fatalf("page up without window: cursor = %d, want 0 (home)", l.Cursor())
	}
}

// TestSelectVisibleEmptyIsNoOp：空可见集全选是显式 no-op（审查 list.go:239）。
func TestSelectVisibleEmptyIsNoOp(t *testing.T) {
	var l List
	l.Toggle("a")
	l.SelectVisible(nil)
	if l.SelectionCount() != 1 || !l.IsSelected("a") {
		t.Fatalf("empty SelectVisible changed selection: %d", l.SelectionCount())
	}
}

// TestConfigCreateFormValidatesGroup：create 表单 group 字段复用 meta 编辑
// 的分组校验——非法分组名阻断提交（审查 config:727）。
func TestConfigCreateFormValidatesGroup(t *testing.T) {
	tab := newLoadedCfgTab(t, "alpha")
	out, _ := tab.Update(runeKey("n"))
	ct := out.(*configTab)
	if ct.form == nil {
		t.Fatal("create form did not open")
	}
	var validate func(string) error
	for _, f := range ct.form.fields {
		if f.key == "group" {
			validate = f.validate
		}
	}
	if validate == nil {
		t.Fatal("group field missing from create form")
	}
	if err := validate("bad/name"); err == nil {
		t.Fatal("invalid group name accepted")
	}
	if err := validate(""); err != nil {
		t.Fatalf("empty group should fall back to default, got %v", err)
	}
}
