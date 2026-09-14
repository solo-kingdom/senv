package tui

import "testing"

// TestGroupBarExpandsHints 验证底栏展开非 NoBar 的 Hint，跳过导航键，尾部固定 `?`。
func TestGroupBarExpandsHints(t *testing.T) {
	bindings := []KeyAction{
		actUp, actDown, // Navigate · NoBar
		{[]string{"e"}, "edit", grpItem, false},
		{[]string{"t"}, "toggle group active", grpGroup, false},
		actFilter,
		{[]string{"m"}, "metadata", grpItem, true}, // NoBar 次要键
		actRefresh, // NoBar
	}
	got := groupBar(bindings)
	want := "e edit · t toggle group active · / filter · ?"
	if got != want {
		t.Fatalf("groupBar() = %q, want %q", got, want)
	}
}

func TestGroupBarEmptyBindings(t *testing.T) {
	got := groupBar(nil)
	want := "?"
	if got != want {
		t.Fatalf("groupBar(nil) = %q, want %q", got, want)
	}
}
