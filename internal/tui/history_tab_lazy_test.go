package tui

import (
	"testing"
)

// TestHistoryTabLazyLoad 验证延迟加载语义：启动批量 Init 不发查询，首次
// 激活（visited）后才装载；未激活时 Reload 只失效缓存不查询。
func TestHistoryTabLazyLoad(t *testing.T) {
	src := &fakeHistorySource{rows: sampleHistoryRows()}
	tab := newHistoryTab(src)

	if cmd := tab.Init(); cmd != nil {
		t.Fatal("未激活时 Init 不应返回装载命令")
	}
	if cmd := tab.Reload(); cmd != nil {
		t.Fatal("未激活时 Reload 不应返回装载命令")
	}

	tab.visited = true
	cmd := tab.Init()
	if cmd == nil {
		t.Fatal("首次激活后 Init 应返回装载命令")
	}
	msg := cmd()
	lm, ok := msg.(historyLoadedMsg)
	if !ok {
		t.Fatalf("装载消息类型不符: %T", msg)
	}
	if len(lm.rows) != len(src.rows) {
		t.Fatalf("装载行数 = %d, want %d", len(lm.rows), len(src.rows))
	}
	tab.Update(lm) // 真实流程中 loaded 由 Update 置位

	// 已装载后重复 Init 不再查询
	if cmd := tab.Init(); cmd != nil {
		t.Fatal("已装载后 Init 不应再次查询")
	}
}
