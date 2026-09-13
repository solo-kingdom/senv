package provider

import (
	"context"
	"testing"
)

// TestCollectMachineLocalArtifactTombstoneCleanup 验证：当本地收集已排除
// 机器本地工件、而同步 state 仍记录远端存在旧版本误收的 config/tui-snapshot
// 时，一次同步会以删除标记清理远端条目并从本地 state 移除，且不触发状态
// 防退化拒写。
func TestCollectMachineLocalArtifactTombstoneCleanup(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	ctx := context.Background()

	// 远端已有旧版本误推的条目。
	pushed, _, err := srv.Push(ctx, "main", []Entry{{
		Kind: KindConfig, Grp: "", Key: "tui-snapshot",
		Ciphertext: []byte("legacy"), BaseRevision: 0,
	}})
	if err != nil {
		t.Fatalf("seed remote: %v", err)
	}
	id := entryID(KindConfig, "", "tui-snapshot")

	// 本地 state 记录该条目，但本地不采集（机器本地工件被排除）。
	st, err := cache.loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	st.Entries[id] = syncEntryState{Revision: pushed[0].Revision, Hash: "legacy"}
	if err := cache.saveState(st); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	if _, err := p.SyncWithReport(ctx); err != nil {
		t.Fatalf("SyncWithReport: %v", err)
	}

	remote, ok := srv.entries["main"][id]
	if !ok || !remote.Deleted {
		t.Fatalf("remote machine-local artifact not tombstoned: %+v", remote)
	}
	st2, err := cache.loadState()
	if err != nil {
		t.Fatalf("loadState after sync: %v", err)
	}
	if _, ok := st2.Entries[id]; ok {
		t.Fatal("state still records machine-local artifact after cleanup")
	}
}
