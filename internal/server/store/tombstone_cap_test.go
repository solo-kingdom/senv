package store

import (
	"context"
	"strings"
	"testing"

	"github.com/wii/senv/internal/server/testdb"
)

// 单批 tombstone 上限：默认关闭，配置后超限整批拒绝（防失陷 client 一次抹掉大量条目）。
func TestPushTombstoneCap(t *testing.T) {
	s := NewSQL(testdb.New(t))
	ctx := context.Background()
	userID, _ := newTestUser(t, s, "alice")

	revs := map[string]int64{}
	for _, k := range []string{"A", "B", "C"} {
		pushed, _, err := s.PushEntries(ctx, userID, 0, "main", []Entry{
			{Kind: "env", Grp: "g", Key: k, Ciphertext: []byte("v1")},
		})
		if err != nil {
			t.Fatalf("create %s: %v", k, err)
		}
		revs[k] = pushed[0].Revision
	}

	del := func(keys ...string) error {
		entries := make([]Entry, 0, len(keys))
		for _, k := range keys {
			entries = append(entries, Entry{Kind: "env", Grp: "g", Key: k, BaseRevision: revs[k], Deleted: true})
		}
		pushed, _, err := s.PushEntries(ctx, userID, 0, "main", entries)
		if err == nil {
			for i, k := range keys {
				revs[k] = pushed[i].Revision
			}
		}
		return err
	}
	put := func(k, value string) {
		pushed, _, err := s.PushEntries(ctx, userID, 0, "main", []Entry{
			{Kind: "env", Grp: "g", Key: k, BaseRevision: revs[k], Ciphertext: []byte(value)},
		})
		if err != nil {
			t.Fatalf("put %s: %v", k, err)
		}
		revs[k] = pushed[0].Revision
	}

	// 未配置上限：一批删三条允许（旧行为）
	if err := del("A", "B", "C"); err != nil {
		t.Fatalf("unlimited delete batch: %v", err)
	}

	// 重建三条并配置上限 2
	for _, k := range []string{"A", "B", "C"} {
		put(k, "v2")
	}
	s.SetMaxTombstonesPerPush(2)
	if err := del("A", "B", "C"); err == nil || !strings.Contains(err.Error(), "删除条目数") {
		t.Fatalf("cap=2 delete 3 err = %v, want tombstone cap error", err)
	}
	// 上限内允许
	if err := del("A", "B"); err != nil {
		t.Fatalf("cap=2 delete 2: %v", err)
	}
}
