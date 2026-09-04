package tui

import "testing"

func TestVisibleRangeFitsShortLists(t *testing.T) {
	start, end := visibleRange(3, 1, 10)
	if start != 0 || end != 3 {
		t.Errorf("visibleRange(3,1,10) = [%d,%d), want [0,3)", start, end)
	}
}

func TestVisibleRangeEmpty(t *testing.T) {
	start, end := visibleRange(0, 0, 5)
	if start != 0 || end != 0 {
		t.Errorf("visibleRange empty = [%d,%d), want [0,0)", start, end)
	}
}

func TestVisibleRangeKeepsCursorInWindow(t *testing.T) {
	cases := []struct {
		n, cursor, page int
		wantStart       int
		wantEnd         int
	}{
		{n: 20, cursor: 0, page: 5, wantStart: 0, wantEnd: 5},
		{n: 20, cursor: 4, page: 5, wantStart: 0, wantEnd: 5},
		{n: 20, cursor: 5, page: 5, wantStart: 1, wantEnd: 6},
		{n: 20, cursor: 19, page: 5, wantStart: 15, wantEnd: 20},
		{n: 20, cursor: 18, page: 5, wantStart: 14, wantEnd: 19},
		{n: 5, cursor: 2, page: 5, wantStart: 0, wantEnd: 5},
		{n: 10, cursor: 99, page: 4, wantStart: 6, wantEnd: 10}, // clamp cursor
	}
	for _, c := range cases {
		start, end := visibleRange(c.n, c.cursor, c.page)
		if start != c.wantStart || end != c.wantEnd {
			t.Errorf("visibleRange(%d,%d,%d) = [%d,%d), want [%d,%d)",
				c.n, c.cursor, c.page, start, end, c.wantStart, c.wantEnd)
		}
		if c.n == 0 {
			continue
		}
		cursor := c.cursor
		if cursor < 0 {
			cursor = 0
		}
		if cursor >= c.n {
			cursor = c.n - 1
		}
		if cursor < start || cursor >= end {
			t.Errorf("cursor %d not in window [%d,%d)", cursor, start, end)
		}
	}
}

func TestClipLines(t *testing.T) {
	in := "a\nb\nc\nd"
	if got := clipLines(in, 2); got != "a\nb" {
		t.Errorf("clipLines = %q, want %q", got, "a\nb")
	}
	if got := clipLines(in, 10); got != in {
		t.Errorf("clipLines no-op = %q", got)
	}
}

func TestWindowedPaneKeepsCursorVisible(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = string(rune('A' + i))
	}
	out := windowedPane("Groups (20)", lines, 19, 6, 40)
	if !contains(out, "T") { // 20th item, cursor at 19
		t.Errorf("cursor line missing:\n%s", out)
	}
	if contains(out, "A") {
		t.Errorf("scrolled-away first line still visible:\n%s", out)
	}
	if !contains(out, "16–20") {
		t.Errorf("expected visible range in title:\n%s", out)
	}
	if h := countLines(out); h > 6 {
		t.Errorf("windowed pane height %d exceeds 6:\n%s", h, out)
	}
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := 1
	for _, r := range s {
		if r == '\n' {
			n++
		}
	}
	return n
}
