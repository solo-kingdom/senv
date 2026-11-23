package store

import (
	"context"
	"testing"
	"time"

	"github.com/wii/senv/internal/server/testdb"
)

// pushEntry 是测试辅助：以指定 base_revision 推送单条
func pushEntry(t *testing.T, s *pgStore, userID int64, vault string, kind, grp, key string, rev int64, ciphertext []byte) {
	t.Helper()
	_, _, err := s.PushEntries(context.Background(), userID, 0, vault, []Entry{
		{Kind: kind, Grp: grp, Key: key, Ciphertext: ciphertext, BaseRevision: rev},
	})
	if err != nil {
		t.Fatalf("push %s/%s/%s rev %d: %v", kind, grp, key, rev, err)
	}
}

func TestEntryHistoryRetention(t *testing.T) {
	s := NewSQL(testdb.New(t))
	ctx := context.Background()
	userID, _ := newTestUser(t, s, "alice")

	for i := 1; i <= 5; i++ {
		pushEntry(t, s, userID, "main", "env", "deploy", "KEY", int64(i-1), []byte{byte(i)})
	}
	// 当前值 = 第 5 版；历史保留最近 3 版（第 4、3、2 版的修改前值 = 4、3、2）
	history, err := s.ListHistory(ctx, userID, "main", HistoryFilter{Kind: "env", Grp: "deploy", Key: "KEY"})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("history len = %d, want 3 (retain)", len(history))
	}
	// revision 新到旧：第 4 版修改前的值是 revision 4 的行
	for i, h := range history {
		wantRev := int64(4 - i)
		if h.Revision != wantRev || len(h.Ciphertext) != 1 || h.Ciphertext[0] != byte(wantRev) {
			t.Errorf("history[%d] = rev %d value %v, want rev %d value [%d]", i, h.Revision, h.Ciphertext, wantRev, wantRev)
		}
		if h.Deleted {
			t.Errorf("history[%d] should not be deleted", i)
		}
	}
}

func TestEntryHistoryDisabled(t *testing.T) {
	s := NewSQL(testdb.New(t))
	s.SetHistoryRetain(0)
	ctx := context.Background()
	userID, _ := newTestUser(t, s, "alice")

	pushEntry(t, s, userID, "main", "env", "g", "K", 0, []byte("v1"))
	pushEntry(t, s, userID, "main", "env", "g", "K", 1, []byte("v2"))
	history, err := s.ListHistory(ctx, userID, "main", HistoryFilter{Kind: "env", Grp: "g", Key: "K"})
	if err != nil || len(history) != 0 {
		t.Fatalf("history with retain=0 = %d, %v; want 0", len(history), err)
	}
}

func TestEntryHistoryDeleteRecoverable(t *testing.T) {
	s := NewSQL(testdb.New(t))
	ctx := context.Background()
	userID, _ := newTestUser(t, s, "alice")

	pushEntry(t, s, userID, "main", "env", "g", "K", 0, []byte("v1"))
	// 推送删除：删除前像（v1）必须进入历史
	_, _, err := s.PushEntries(ctx, userID, 0, "main", []Entry{
		{Kind: "env", Grp: "g", Key: "K", BaseRevision: 1, Deleted: true},
	})
	if err != nil {
		t.Fatalf("push delete: %v", err)
	}
	history, err := s.ListHistory(ctx, userID, "main", HistoryFilter{Kind: "env", Grp: "g", Key: "K"})
	if err != nil || len(history) != 1 {
		t.Fatalf("history after delete = %d, %v; want 1", len(history), err)
	}
	if history[0].Deleted || string(history[0].Ciphertext) != "v1" {
		t.Errorf("pre-delete history = %+v, want alive v1", history[0])
	}

	// 从历史恢复（重新创建）：base_revision = 墓碑 revision 2
	_, _, err = s.PushEntries(ctx, userID, 0, "main", []Entry{
		{Kind: "env", Grp: "g", Key: "K", BaseRevision: 2, Ciphertext: []byte("v1")},
	})
	if err != nil {
		t.Fatalf("restore deleted entry: %v", err)
	}
	history, _ = s.ListHistory(ctx, userID, "main", HistoryFilter{Kind: "env", Grp: "g", Key: "K"})
	// 恢复推送又产生一条前像（墓碑行 deleted=true），随后新值版本也在
	if len(history) < 1 {
		t.Fatalf("history after restore should not be empty")
	}
}

func TestListHistoryVaultRecent(t *testing.T) {
	s := NewSQL(testdb.New(t))
	ctx := context.Background()
	userID, _ := newTestUser(t, s, "alice")

	// 首次创建无前像（没有旧值可留）；两次修改各产生一条历史
	pushEntry(t, s, userID, "main", "env", "g1", "K", 0, []byte("v1"))
	pushEntry(t, s, userID, "main", "env", "g1", "K", 1, []byte("v2"))
	pushEntry(t, s, userID, "main", "text", "notes", "README", 0, []byte("t1"))
	pushEntry(t, s, userID, "main", "env", "g1", "K", 2, []byte("v3"))

	all, err := s.ListHistory(ctx, userID, "main", HistoryFilter{})
	if err != nil || len(all) != 2 {
		t.Fatalf("vault recent history = %d, %v; want 2 (creations have no pre-image)", len(all), err)
	}
	// vault 级按 created_at DESC：最近修改（v3 的前像 rev2）在最前
	if all[0].Revision != 2 || string(all[0].Ciphertext) != "v2" {
		t.Errorf("top recent = rev %d %q, want rev 2 v2", all[0].Revision, all[0].Ciphertext)
	}

	// 跨用户隔离：bob 的 main vault 不存在 → ErrNotFound（HTTP 层映射 404）
	otherID, _ := newTestUser(t, s, "bob")
	if _, err := s.ListHistory(ctx, otherID, "main", HistoryFilter{}); err == nil {
		t.Errorf("cross-user vault query should return ErrNotFound")
	}
}

// 相同密文重推、tombstone 重推不消耗历史版本（否则只持 token 即可清历史）。
func TestEntryHistorySkipsUnchangedRepush(t *testing.T) {
	s := NewSQL(testdb.New(t))
	s.SetHistoryRetain(3)
	ctx := context.Background()
	userID, _ := newTestUser(t, s, "alice")

	pushEntry(t, s, userID, "main", "env", "g", "K", 0, []byte("v1"))
	pushEntry(t, s, userID, "main", "env", "g", "K", 1, []byte("v2")) // 前像 v1
	pushEntry(t, s, userID, "main", "env", "g", "K", 2, []byte("v2")) // 相同密文重推：不应新增
	pushEntry(t, s, userID, "main", "env", "g", "K", 3, []byte("v2")) // 仍不应新增

	history, err := s.ListHistory(ctx, userID, "main", HistoryFilter{Kind: "env", Grp: "g", Key: "K"})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || string(history[0].Ciphertext) != "v1" {
		t.Fatalf("history = %+v, want only the v1 pre-image", history)
	}

	// 删除后重复推 tombstone：只留一条墓碑前像，真实最后一版不被挤掉。
	_, _, err = s.PushEntries(ctx, userID, 0, "main", []Entry{{Kind: "env", Grp: "g", Key: "K", BaseRevision: 4, Deleted: true}})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.PushEntries(ctx, userID, 0, "main", []Entry{{Kind: "env", Grp: "g", Key: "K", BaseRevision: 5, Deleted: true}})
	if err != nil {
		t.Fatal(err)
	}
	history, _ = s.ListHistory(ctx, userID, "main", HistoryFilter{Kind: "env", Grp: "g", Key: "K"})
	// 期望：v1 前像 + 首次删除留下的 v2 前像（rev4）；重复删除不再新增
	// （第二次删除的前像是墓碑行，与新值一致，不写）。
	if len(history) != 2 {
		t.Fatalf("history after repeated tombstones = %d rows (%+v), want 2", len(history), history)
	}
	foundV2 := false
	for _, h := range history {
		if !h.Deleted && string(h.Ciphertext) == "v2" {
			foundV2 = true
		}
	}
	if !foundV2 {
		t.Fatalf("last real version v2 evicted from history: %+v", history)
	}
}

// 历史行记录服务端认定的操作者（client/user）与变更时刻。
func TestEntryHistoryActor(t *testing.T) {
	s := NewSQL(testdb.New(t))
	ctx := context.Background()
	userID, _ := newTestUser(t, s, "alice")

	code, err := s.CreateRegistrationCode(ctx, userID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, client, err := s.RegisterClient(ctx, code, "dev-a")
	if err != nil {
		t.Fatal(err)
	}

	pushEntry(t, s, userID, "main", "env", "g", "K", 0, []byte("v1"))
	if _, _, err := s.PushEntries(ctx, userID, client.ID, "main", []Entry{
		{Kind: "env", Grp: "g", Key: "K", BaseRevision: 1, Ciphertext: []byte("v2")},
	}); err != nil {
		t.Fatal(err)
	}

	history, err := s.ListHistory(ctx, userID, "main", HistoryFilter{Kind: "env", Grp: "g", Key: "K"})
	if err != nil || len(history) != 1 {
		t.Fatalf("history = %d, %v; want 1", len(history), err)
	}
	h := history[0]
	if h.ActorClientID != client.ID || h.ActorUserID != userID {
		t.Errorf("actor = client %d user %d, want %d/%d", h.ActorClientID, h.ActorUserID, client.ID, userID)
	}
	if h.ActorClientName != "dev-a" {
		t.Errorf("actor client name = %q, want dev-a", h.ActorClientName)
	}
	if h.ReplacedAt.IsZero() {
		t.Error("replaced_at should be set")
	}
}

// admin 侧裁剪：把所有 vault 的历史裁到指定版本数。
func TestPruneAllHistory(t *testing.T) {
	s := NewSQL(testdb.New(t))
	s.SetHistoryRetain(10)
	ctx := context.Background()
	userID, _ := newTestUser(t, s, "alice")

	for i := 1; i <= 6; i++ {
		pushEntry(t, s, userID, "main", "env", "g", "K", int64(i-1), []byte{byte(i)})
	}
	if h, _ := s.ListHistory(ctx, userID, "main", HistoryFilter{Kind: "env", Grp: "g", Key: "K"}); len(h) != 5 {
		t.Fatalf("history before prune = %d, want 5", len(h))
	}
	n, err := s.PruneAllHistory(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("pruned rows = %d, want 3", n)
	}
	if h, _ := s.ListHistory(ctx, userID, "main", HistoryFilter{Kind: "env", Grp: "g", Key: "K"}); len(h) != 2 {
		t.Fatalf("history after prune = %d, want 2", len(h))
	}
}
