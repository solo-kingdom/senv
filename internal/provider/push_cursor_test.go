package provider

import (
	"context"
	"os"
	"testing"
)

func TestAdvanceCursorAfterPush(t *testing.T) {
	tests := []struct {
		name   string
		cur    int64
		pushed []Entry
		latest int64
		want   int64
	}{
		{
			name:   "contiguous single push advances",
			cur:    10,
			pushed: []Entry{{Revision: 11}},
			latest: 11,
			want:   11,
		},
		{
			name:   "contiguous batch advances",
			cur:    10,
			pushed: []Entry{{Revision: 11}, {Revision: 12}, {Revision: 13}},
			latest: 13,
			want:   13,
		},
		{
			name:   "gap before pushed keeps cursor",
			cur:    5653,
			pushed: []Entry{{Revision: 6185}},
			latest: 6185,
			want:   5653,
		},
		{
			name:   "gap inside multi-batch keeps cursor",
			cur:    10,
			pushed: []Entry{{Revision: 11}, {Revision: 13}},
			latest: 13,
			want:   10,
		},
		{
			name:   "duplicate revision keeps cursor",
			cur:    10,
			pushed: []Entry{{Revision: 11}, {Revision: 11}},
			latest: 12,
			want:   10,
		},
		{
			name:   "empty push keeps cursor",
			cur:    10,
			pushed: nil,
			latest: 15,
			want:   10,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := advanceCursorAfterPush(tc.cur, tc.pushed, tc.latest); got != tc.want {
				t.Fatalf("advanceCursorAfterPush(%d, %+v, %d) = %d, want %d", tc.cur, tc.pushed, tc.latest, got, tc.want)
			}
		})
	}
}

// TestAutoPushBehindRemoteDoesNotSkipUnpulledRevisions 复现线上事故：
// 本机未拉取远端区间 (cur, remoteLatest) 时触发 AutoPush 成功，
// 不得把 LastSyncedRevision 跳到 server latest；下次 Sync 必须完整补回缺失条目与删除。
func TestAutoPushBehindRemoteDoesNotSkipUnpulledRevisions(t *testing.T) {
	ctx := context.Background()
	srv := newFakeServer()
	srv.metadata["main"] = []byte(`{"salt":"s","password_key":"k"}`)

	machineA := newServerProvider(srv, t.TempDir(), t.TempDir(), "main")
	machineB := newServerProvider(srv, t.TempDir(), t.TempDir(), "main")
	if err := machineA.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if err := machineB.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}

	// 基线：A 写入 OLD_KEY 并同步到 B
	writeEnvVar(t, machineA.cache, "default", "OLD_KEY", "old-val")
	if _, err := machineA.SyncWithReport(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := machineB.SyncWithReport(ctx); err != nil {
		t.Fatal(err)
	}
	assertStateRevision(t, machineB.cache, 1)

	// A 连续写入新条目并删除 OLD_KEY（模拟 iship 的大批量改动）
	if err := os.Remove(mustEntryPath(t, machineA.cache, KindEnv, "default", "OLD_KEY")); err != nil {
		t.Fatal(err)
	}
	writeEnvVar(t, machineA.cache, "default", "NEW_1", "v1")
	writeEnvVar(t, machineA.cache, "default", "NEW_2", "v2")
	if _, err := machineA.SyncWithReport(ctx); err != nil {
		t.Fatal(err)
	}

	// B 的 autoPull 超时未执行，直接写入本地条目并触发 AutoPush
	writeEnvVar(t, machineB.cache, "default", "LOCAL_B", "from-b")
	if _, err := machineB.AutoPush(ctx, autoSyncBudget); err != nil {
		t.Fatalf("machine B AutoPush: %v", err)
	}

	// B 的游标必须停在 1（不得跳到 5）
	assertStateRevision(t, machineB.cache, 1)

	// 随后 B 执行正常 Sync：必须拉回 NEW_1、NEW_2，删掉 OLD_KEY，保留 LOCAL_B，且不重复计入 LOCAL_B
	res, err := machineB.SyncWithReport(ctx)
	if err != nil {
		t.Fatalf("machine B SyncWithReport: %v", err)
	}
	if res.Pull.Applied != 3 {
		t.Fatalf("machine B Pull.Applied = %d, want 3 (NEW_1, NEW_2, tombstone OLD_KEY; skip loopback LOCAL_B)", res.Pull.Applied)
	}
	assertStateRevision(t, machineB.cache, 5)

	if _, err := os.Stat(mustEntryPath(t, machineB.cache, KindEnv, "default", "OLD_KEY")); !os.IsNotExist(err) {
		t.Fatalf("OLD_KEY should be deleted on machine B, stat err=%v", err)
	}
	for k, want := range map[string]string{"NEW_1": "v1", "NEW_2": "v2", "LOCAL_B": "from-b"} {
		got, err := os.ReadFile(mustEntryPath(t, machineB.cache, KindEnv, "default", k))
		if err != nil || string(got) != want {
			t.Fatalf("machine B %s = %q (%v), want %q", k, got, err, want)
		}
	}
}

// TestSyncConcurrentRemoteWriteBetweenPullAndPush 验证 pull 与 push 之间插入远端写入时，
// 本机 push 不会把游标越过该并发写入，下次 Sync 可完整拉回。
func TestSyncConcurrentRemoteWriteBetweenPullAndPush(t *testing.T) {
	ctx := context.Background()
	srv := newFakeServer()
	srv.metadata["main"] = []byte(`{"salt":"s","password_key":"k"}`)

	p, cache := newTestProvider(t, srv)
	writeEnvVar(t, cache, "default", "LOCAL_KEY", "local-val")

	injected := false
	srv.beforePush = func() {
		if injected {
			return
		}
		injected = true
		// 模拟 pull 返回后、push 落盘前另一台设备写入了 CONCURRENT_KEY (rev 1)
		srv.seq["main"]++
		srv.entries["main"] = map[string]Entry{
			entryID(KindEnv, "default", "CONCURRENT_KEY"): {
				Kind:       KindEnv,
				Grp:        "default",
				Key:        "CONCURRENT_KEY",
				Ciphertext: []byte("remote-concurrent"),
				Revision:   srv.seq["main"],
			},
		}
	}

	if _, err := p.SyncWithReport(ctx); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	// 并发写入占用了 rev 1，本机 push 拿到 rev 2，存在空洞，游标必须保持 0
	assertStateRevision(t, cache, 0)

	// 下一次 Sync 补拉 rev 1
	res, err := p.SyncWithReport(ctx)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if res.Pull.Applied != 1 {
		t.Fatalf("second sync Pull.Applied = %d, want 1", res.Pull.Applied)
	}
	assertStateRevision(t, cache, 2)
	got, err := os.ReadFile(mustEntryPath(t, cache, KindEnv, "default", "CONCURRENT_KEY"))
	if err != nil || string(got) != "remote-concurrent" {
		t.Fatalf("CONCURRENT_KEY = %q (%v), want remote-concurrent", got, err)
	}
}

// TestRestoreForcePushAndResolveDoNotSkipRemoteRevisions 覆盖其余三处 push 路径：
// RestoreEntry、ForcePush、ResolveConflicts 在存在未拉取远端变更时都不得推进游标跳过它们。
func TestRestoreForcePushAndResolveDoNotSkipRemoteRevisions(t *testing.T) {
	ctx := context.Background()

	t.Run("RestoreEntry", func(t *testing.T) {
		srv := newFakeServer()
		srv.metadata["main"] = []byte(`{"salt":"s","password_key":"k"}`)
		p, cache := newTestProvider(t, srv)

		// 远端先有 2 条本机未拉取的条目
		if _, _, err := srv.Push(ctx, "main", []Entry{
			{Kind: KindEnv, Grp: "default", Key: "OTHER", Ciphertext: []byte("other")},
			{Kind: KindEnv, Grp: "default", Key: "TARGET", Ciphertext: []byte("v1")},
		}); err != nil {
			t.Fatal(err)
		}
		if err := p.RestoreEntry(ctx, KindEnv, "default", "TARGET", []byte("restored")); err != nil {
			t.Fatalf("RestoreEntry: %v", err)
		}
		assertStateRevision(t, cache, 0)

		if _, err := p.SyncWithReport(ctx); err != nil {
			t.Fatalf("SyncWithReport after RestoreEntry: %v", err)
		}
		got, err := os.ReadFile(mustEntryPath(t, cache, KindEnv, "default", "OTHER"))
		if err != nil || string(got) != "other" {
			t.Fatalf("OTHER = %q (%v), want other", got, err)
		}
	})

	t.Run("ForcePush", func(t *testing.T) {
		srv := newFakeServer()
		srv.metadata["main"] = []byte(`{"salt":"s","password_key":"k"}`)
		p, cache := newTestProvider(t, srv)

		if _, _, err := srv.Push(ctx, "main", []Entry{
			{Kind: KindEnv, Grp: "default", Key: "OTHER", Ciphertext: []byte("other")},
		}); err != nil {
			t.Fatal(err)
		}
		writeEnvVar(t, cache, "default", "LOCAL", "mine")
		if err := p.ForcePush(ctx); err != nil {
			t.Fatalf("ForcePush: %v", err)
		}
		assertStateRevision(t, cache, 0)

		if _, err := p.SyncWithReport(ctx); err != nil {
			t.Fatalf("SyncWithReport after ForcePush: %v", err)
		}
		got, err := os.ReadFile(mustEntryPath(t, cache, KindEnv, "default", "OTHER"))
		if err != nil || string(got) != "other" {
			t.Fatalf("OTHER = %q (%v), want other", got, err)
		}
	})

	t.Run("ResolveConflicts", func(t *testing.T) {
		srv := newFakeServer()
		srv.metadata["main"] = []byte(`{"salt":"s","password_key":"k"}`)
		p, cache := newTestProvider(t, srv)

		// 远端有冲突条目 SHARED 与未拉取的非冲突条目 OTHER
		pushed, _, err := srv.Push(ctx, "main", []Entry{
			{Kind: KindEnv, Grp: "default", Key: "OTHER", Ciphertext: []byte("other")},
			{Kind: KindEnv, Grp: "default", Key: "SHARED", Ciphertext: []byte("remote-shared")},
		})
		if err != nil {
			t.Fatal(err)
		}
		writeEnvVar(t, cache, "default", "SHARED", "local-shared")

		plan := ResolutionPlan{
			Items: []ConflictResolution{{
				Kind:                   KindEnv,
				Grp:                    "default",
				Key:                    "SHARED",
				Decision:               ResolutionLocal,
				ExpectedLocalHash:      hashBytes([]byte("local-shared")),
				ExpectedRemoteRevision: pushed[1].Revision,
			}},
		}
		if err := p.ResolveConflicts(ctx, plan); err != nil {
			t.Fatalf("ResolveConflicts: %v", err)
		}
		assertStateRevision(t, cache, 0)

		if _, err := p.SyncWithReport(ctx); err != nil {
			t.Fatalf("SyncWithReport after ResolveConflicts: %v", err)
		}
		got, err := os.ReadFile(mustEntryPath(t, cache, KindEnv, "default", "OTHER"))
		if err != nil || string(got) != "other" {
			t.Fatalf("OTHER = %q (%v), want other", got, err)
		}
	})
}
