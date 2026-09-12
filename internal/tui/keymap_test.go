package tui

import "testing"

// TestGroupBarCollapsesGroups 验证底栏提示按组名首现顺序去重，且尾部固定
// `? keys` 指向完整键位总览；空 Group 回落到 "Keys"。
func TestGroupBarCollapsesGroups(t *testing.T) {
	bindings := []KeyAction{
		actUp, actDown, // Navigate
		{[]string{"e"}, "edit", grpItem},
		{[]string{"t"}, "toggle group active", grpGroup},
		actFilter, // Filter
		{[]string{"e"}, "edit", grpItem},
		{[]string{"?"}, "misc", ""}, // 空 Group → Keys
	}
	got := groupBar(bindings)
	want := "Navigate · Items · Groups · Filter · Keys · ? keys"
	if got != want {
		t.Fatalf("groupBar() = %q, want %q", got, want)
	}
}
