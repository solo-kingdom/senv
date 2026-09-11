package tui

import "testing"

func TestListComponentVisibleRange(t *testing.T) {
	cases := []struct {
		name           string
		n, cursor, h   int
		wantStart, end int
	}{
		{"空列表", 0, 0, 5, 0, 0},
		{"不窗口化", 10, 8, 0, 0, 10},
		{"单行", 1, 0, 5, 0, 1},
		{"顶部", 10, 0, 5, 0, 4},
		{"中部跟随", 10, 4, 5, 1, 5},
		{"底部收拢", 10, 9, 5, 6, 10},
		{"游标越界被收拢", 10, 99, 5, 6, 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := List{height: c.h}
			l.SetCursor(c.cursor, c.n)
			start, end := l.VisibleRange(c.n)
			if start != c.wantStart || end != c.end {
				t.Fatalf("VisibleRange(%d) cursor=%d h=%d = [%d,%d), want [%d,%d)",
					c.n, c.cursor, c.h, start, end, c.wantStart, c.end)
			}
		})
	}
}

func TestListComponentMovePageHomeEnd(t *testing.T) {
	l := List{height: 4} // listPageSize = 3
	l.SetCursor(1, 10)
	l.Move(-5, 10)
	if l.Cursor() != 0 {
		t.Fatalf("move below 0 clamped, got %d", l.Cursor())
	}
	l.Page(1, 10) // 0 -> 3
	if l.Cursor() != 3 {
		t.Fatalf("page down = %d, want 3", l.Cursor())
	}
	l.Page(1, 10) // 3 -> 6
	l.Page(1, 10) // 6 -> 9
	l.Page(1, 10) // 9 (clamped)
	if l.Cursor() != 9 {
		t.Fatalf("page down end = %d, want 9", l.Cursor())
	}
	l.Home()
	if l.Cursor() != 0 {
		t.Fatalf("home = %d", l.Cursor())
	}
	l.End(10)
	if l.Cursor() != 9 {
		t.Fatalf("end = %d, want 9", l.Cursor())
	}
	l.End(0) // 空列表：游标保持不变
	if l.Cursor() != 9 {
		t.Fatalf("end on empty should keep cursor, got %d", l.Cursor())
	}
	l.SetCursor(5, 0) // 空列表游标归零
	if l.Cursor() != 0 {
		t.Fatalf("cursor on empty = %d, want 0", l.Cursor())
	}
}

func TestPaneBudgets(t *testing.T) {
	// env/text/config 形态：1/4、clamp 16–26
	left, right := paneBudgets(120, 1, 4, 16, 26)
	if left != 26 || right != 120-26-5 {
		t.Fatalf("quarter budgets = (%d,%d)", left, right)
	}
	// ssh 形态：11/20
	left, right = paneBudgets(100, 11, 20, 1, 1000)
	if left != 55 || right != 100-55-5 {
		t.Fatalf("ssh budgets = (%d,%d)", left, right)
	}
	// ai/mcp 形态：2/5
	left, _ = paneBudgets(100, 2, 5, 1, 1000)
	if left != 40 {
		t.Fatalf("ai budgets left = %d, want 40", left)
	}
	// 窄终端：左栏抬到下限、右栏保底 4
	left, right = paneBudgets(30, 1, 4, 16, 26)
	if left != 16 || right != 9 {
		t.Fatalf("narrow budgets = (%d,%d)", left, right)
	}
}
