package text

import (
	"testing"
)

// TestSnapshotMatchesListPath 验证 Snapshot（单趟批量读取）与逐组
// ListGroups/List 路径返回的分组与条目元数据完全一致（tui-startup-perf
// D2 的正确性前提），且空分组也被包含。
func TestSnapshotMatchesListPath(t *testing.T) {
	mgr, _ := setupTestTextManager(t)

	if err := mgr.AddGroup("zeta", "test"); err != nil {
		t.Fatalf("create empty group zeta: %v", err)
	}
	seed := map[string]map[string]string{
		"default": {"readme": "hello world", "config": "k=v"},
		"prod":    {"token": "secret"},
		"zeta":    {},
	}
	if err := mgr.AddGroup("prod", "test"); err != nil {
		t.Fatalf("create prod: %v", err)
	}
	for group, entries := range seed {
		for k, v := range entries {
			if err := mgr.Set(group, k, v); err != nil {
				t.Fatalf("set %s/%s: %v", group, k, err)
			}
		}
	}

	snap, err := mgr.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// 分组集合与计数对齐 ListGroups
	groups, err := mgr.ListGroups()
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(snap.Groups) != len(groups) {
		t.Fatalf("snapshot groups = %d, ListGroups = %d", len(snap.Groups), len(groups))
	}
	wantCount := make(map[string]int, len(groups))
	for _, g := range groups {
		wantCount[g.Name] = g.KeyCount
	}
	for _, g := range snap.Groups {
		if g.KeyCount != wantCount[g.Name] {
			t.Errorf("group %q KeyCount = %d, ListGroups = %d", g.Name, g.KeyCount, wantCount[g.Name])
		}
	}

	// 每组条目元数据对齐 List（key 集合 + size + updatedAt）
	for name := range seed {
		infos, err := mgr.List(name)
		if err != nil {
			t.Fatalf("List(%q): %v", name, err)
		}
		got := snap.Items[name]
		if len(got) != len(infos) {
			t.Fatalf("group %q snapshot items = %d, List = %d", name, len(got), len(infos))
		}
		byKey := make(map[string]TextInfo, len(got))
		for _, ti := range got {
			byKey[ti.Key] = ti
		}
		for _, wantInfo := range infos {
			gi, ok := byKey[wantInfo.Key]
			if !ok {
				t.Fatalf("snapshot missing %s/%s", name, wantInfo.Key)
			}
			if gi.Size != wantInfo.Size || !gi.UpdatedAt.Equal(wantInfo.UpdatedAt) {
				t.Errorf("%s/%s meta: snapshot=(%d,%s) list=(%d,%s)", name, wantInfo.Key,
					gi.Size, gi.UpdatedAt, wantInfo.Size, wantInfo.UpdatedAt)
			}
		}
	}

	// 单条目内容正确（Snapshot 读取解密链路无损坏）
	if v, err := mgr.Get("prod", "token"); err != nil || v != "secret" {
		t.Fatalf("prod/token = %q, %v; want secret", v, err)
	}
}

// TestSnapshotSeesNewWrites 验证 Snapshot 无跨调用缓存：写入后再次
// Snapshot 必须看到新条目。
func TestSnapshotSeesNewWrites(t *testing.T) {
	mgr, _ := setupTestTextManager(t)
	if err := mgr.Set("default", "A", "1"); err != nil {
		t.Fatal(err)
	}
	snap1, err := mgr.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Set("default", "B", "2"); err != nil {
		t.Fatal(err)
	}
	snap2, err := mgr.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap2.Items["default"]) != len(snap1.Items["default"])+1 {
		t.Fatalf("second snapshot items = %d, want %d", len(snap2.Items["default"]), len(snap1.Items["default"])+1)
	}
	found := false
	for _, ti := range snap2.Items["default"] {
		if ti.Key == "B" {
			found = true
		}
	}
	if !found {
		t.Fatal("new key B missing from second snapshot")
	}
}
