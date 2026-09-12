package store

import (
	"context"
	"testing"

	"github.com/wii/senv/internal/server/testdb"
)

// pushEntry 是测试辅助：以指定 base_revision 推送单条
func pushEntry(t *testing.T, s *pgStore, userID int64, vault string, kind, grp, key string, rev int64, ciphertext []byte) {
	t.Helper()
	_, _, err := s.PushEntries(context.Background(), userID, vault, []Entry{
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
	_, _, err := s.PushEntries(ctx, userID, "main", []Entry{
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
	_, _, err = s.PushEntries(ctx, userID, "main", []Entry{
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
