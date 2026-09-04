package store

import (
	"context"
	"errors"
	"testing"

	"github.com/wii/senv/internal/server/testdb"
)

// newStore 创建测试用 Store
func newStore(t *testing.T) *Store {
	return New(testdb.New(t))
}

// newUser 创建用户并认证回 user_id
func newUser(t *testing.T, st *Store, name string) int64 {
	t.Helper()
	ctx := context.Background()
	token, err := st.CreateUser(ctx, name)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := st.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	return userID
}

func TestTokenLifecycle(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	token, err := st.CreateUser(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := st.Authenticate(ctx, token); err != nil {
		t.Fatalf("Authenticate valid token: %v", err)
	}

	// 无效 token 与吊销后的 token 返回一致的 ErrNotFound（不泄露存在性）
	if _, err := st.Authenticate(ctx, "bogus"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Authenticate bogus = %v, want ErrNotFound", err)
	}
	if err := st.RevokeToken(ctx, token); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if _, err := st.Authenticate(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Errorf("Authenticate revoked = %v, want ErrNotFound", err)
	}
}

func TestMetadataRoundtrip(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	userID := newUser(t, st, "bob")

	// 写入即建 vault；读取原样返回 blob
	blob := []byte("opaque-encrypted-metadata")
	if err := st.PutMetadata(ctx, userID, "main", blob); err != nil {
		t.Fatalf("PutMetadata: %v", err)
	}
	got, err := st.GetMetadata(ctx, userID, "main")
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if string(got) != string(blob) {
		t.Errorf("metadata roundtrip = %q, want %q", got, blob)
	}

	// 覆盖写
	if err := st.PutMetadata(ctx, userID, "main", []byte("v2")); err != nil {
		t.Fatalf("PutMetadata v2: %v", err)
	}
	got, _ = st.GetMetadata(ctx, userID, "main")
	if string(got) != "v2" {
		t.Errorf("metadata after overwrite = %q, want v2", got)
	}

	// 不存在的 vault → ErrNotFound
	if _, err := st.GetMetadata(ctx, userID, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetMetadata missing vault = %v, want ErrNotFound", err)
	}
}

func TestPushPullAndConflict(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	userID := newUser(t, st, "carol")

	// 无冲突推送：新条目 base_revision = 0
	entries := []Entry{
		{Kind: "env", Grp: "default", Key: "A", Ciphertext: []byte("ca"), BaseRevision: 0},
		{Kind: "env", Grp: "default", Key: "B", Ciphertext: []byte("cb"), BaseRevision: 0},
	}
	pushed, latest, err := st.PushEntries(ctx, userID, "main", entries)
	if err != nil {
		t.Fatalf("PushEntries: %v", err)
	}
	if latest != 2 {
		t.Errorf("latest = %d, want 2", latest)
	}
	// revision 单调递增且不重用
	if pushed[0].Revision >= pushed[1].Revision {
		t.Errorf("revisions not monotonic: %v, %v", pushed[0].Revision, pushed[1].Revision)
	}

	// 增量拉取：since=0 拿到全部；since=latest 拿到空增量
	got, latestPull, err := st.PullEntries(ctx, userID, "main", 0)
	if err != nil {
		t.Fatalf("PullEntries: %v", err)
	}
	if len(got) != 2 || latestPull != 2 {
		t.Errorf("pull since=0: got %d entries, latest=%d; want 2 entries, latest=2", len(got), latestPull)
	}
	for _, e := range got {
		if e.UpdatedAt.IsZero() {
			t.Errorf("pull entry %s updated_at is zero", e.Key)
		}
	}
	got, latestPull, err = st.PullEntries(ctx, userID, "main", 2)
	if err != nil {
		t.Fatalf("PullEntries empty: %v", err)
	}
	if len(got) != 0 || latestPull != 2 {
		t.Errorf("empty increment: got %d entries, latest=%d; want 0 entries, latest=2", len(got), latestPull)
	}

	// 更新：以正确 base_revision 推送
	_, latest, err = st.PushEntries(ctx, userID, "main", []Entry{
		{Kind: "env", Grp: "default", Key: "A", Ciphertext: []byte("ca2"), BaseRevision: 1},
	})
	if err != nil {
		t.Fatalf("PushEntries update: %v", err)
	}
	if latest != 3 {
		t.Errorf("latest after update = %d, want 3", latest)
	}

	// 冲突整批拒绝：A 的 base 落后 + 新条目 C，整批不得写入任何内容
	_, _, err = st.PushEntries(ctx, userID, "main", []Entry{
		{Kind: "env", Grp: "default", Key: "A", Ciphertext: []byte("stale"), BaseRevision: 1},
		{Kind: "env", Grp: "default", Key: "C", Ciphertext: []byte("cc"), BaseRevision: 0},
	})
	var conflictErr *ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("PushEntries stale: got %v, want ConflictError", err)
	}
	if len(conflictErr.Conflicts) != 1 || conflictErr.Conflicts[0].Key != "A" || conflictErr.Conflicts[0].CurrentRevision != 3 {
		t.Errorf("conflict list = %+v, want A@3", conflictErr.Conflicts)
	}
	c := conflictErr.Conflicts[0]
	if c.Deleted || c.Size != int64(len("ca2")) || c.UpdatedAt.IsZero() {
		t.Errorf("conflict descriptor = %+v, want live entry with size %d and updated_at", c, len("ca2"))
	}
	got, _, _ = st.PullEntries(ctx, userID, "main", 0)
	for _, e := range got {
		if e.Key == "C" {
			t.Errorf("partial write detected: entry C must not exist after rejected batch")
		}
		if e.Key == "A" && string(e.Ciphertext) == "stale" {
			t.Errorf("partial write detected: entry A was overwritten by rejected batch")
		}
	}
}

func TestDeleteAdvancesRevision(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	userID := newUser(t, st, "dave")

	st.PushEntries(ctx, userID, "main", []Entry{
		{Kind: "text", Grp: "g", Key: "k", Ciphertext: []byte("c"), BaseRevision: 0},
	})
	// 删除也推进 revision，且之后以旧 since 拉取能观察到删除标记
	_, latest, err := st.PushEntries(ctx, userID, "main", []Entry{
		{Kind: "text", Grp: "g", Key: "k", BaseRevision: 1, Deleted: true},
	})
	if err != nil {
		t.Fatalf("PushEntries delete: %v", err)
	}
	if latest != 2 {
		t.Errorf("latest after delete = %d, want 2", latest)
	}
	got, _, err := st.PullEntries(ctx, userID, "main", 1)
	if err != nil {
		t.Fatalf("PullEntries: %v", err)
	}
	if len(got) != 1 || !got[0].Deleted || got[0].Revision != 2 {
		t.Errorf("pull after delete = %+v, want 1 deleted entry at revision 2", got)
	}
}

func TestVaultIsolation(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	alice := newUser(t, st, "alice2")
	bob := newUser(t, st, "bob2")

	st.PutMetadata(ctx, alice, "main", []byte("alice-meta"))
	st.PushEntries(ctx, alice, "main", []Entry{
		{Kind: "env", Grp: "default", Key: "SECRET", Ciphertext: []byte("x"), BaseRevision: 0},
	})

	// 跨用户访问 vault 一律 ErrNotFound（与不存在时一致）
	if _, err := st.GetMetadata(ctx, bob, "main"); !errors.Is(err, ErrNotFound) {
		t.Errorf("bob GetMetadata alice vault = %v, want ErrNotFound", err)
	}
	if _, _, err := st.PullEntries(ctx, bob, "main", 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("bob PullEntries alice vault = %v, want ErrNotFound", err)
	}
}

func TestPushLimits(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	userID := newUser(t, st, "erin")

	// 单条超过 512KB 上限 → 拒绝并指明超限条目
	big := make([]byte, MaxEntryCiphertext+1)
	_, _, err := st.PushEntries(ctx, userID, "main", []Entry{
		{Kind: "config", Grp: "", Key: "huge", Ciphertext: big, BaseRevision: 0},
	})
	if err == nil {
		t.Fatal("oversized entry should be rejected")
	}

	// 空批次拒绝
	if _, _, err := st.PushEntries(ctx, userID, "main", nil); err == nil {
		t.Error("empty batch should be rejected")
	}
}
