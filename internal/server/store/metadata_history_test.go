package store

import (
	"context"
	"errors"
	"testing"

	"github.com/wii/senv/internal/server/testdb"
)

// metadata 覆盖写留历史、内容未变不耗版本、base_hash 不符返回冲突。
func TestMetadataHistoryAndBaseHash(t *testing.T) {
	s := NewSQL(testdb.New(t))
	ctx := context.Background()
	userID, _ := newTestUser(t, s, "alice")

	// 首次写入：没有前像
	if err := s.PutMetadata(ctx, userID, "main", []byte("meta-v1"), ""); err != nil {
		t.Fatalf("first put: %v", err)
	}
	if h, _ := s.GetMetadataHistory(ctx, userID, "main", 10); len(h) != 0 {
		t.Fatalf("history after first write = %d, want 0", len(h))
	}

	// 第二次写入（带正确 base）：v1 进入历史
	if err := s.PutMetadata(ctx, userID, "main", []byte("meta-v2"), metadataHash([]byte("meta-v1"))); err != nil {
		t.Fatalf("second put: %v", err)
	}
	h, err := s.GetMetadataHistory(ctx, userID, "main", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 1 || string(h[0].Blob) != "meta-v1" || h[0].ReplacedAt.IsZero() {
		t.Fatalf("history = %+v, want one v1 pre-image with replaced_at", h)
	}

	// 相同内容重写：不新增前像
	if err := s.PutMetadata(ctx, userID, "main", []byte("meta-v2"), ""); err != nil {
		t.Fatalf("same put: %v", err)
	}
	if h, _ := s.GetMetadataHistory(ctx, userID, "main", 10); len(h) != 1 {
		t.Fatalf("history after identical write = %d, want 1", len(h))
	}

	// base 不符：冲突且不写入
	err = s.PutMetadata(ctx, userID, "main", []byte("meta-v3"), metadataHash([]byte("meta-v1")))
	var conflict *MetadataConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("stale base err = %v, want MetadataConflictError", err)
	}
	if got, _ := s.GetMetadata(ctx, userID, "main"); string(got) != "meta-v2" {
		t.Fatalf("metadata after conflict = %q, want meta-v2", got)
	}

	// 裁剪：保留最近 2 版
	if err := s.PutMetadata(ctx, userID, "main", []byte("meta-v3"), metadataHash([]byte("meta-v2"))); err != nil {
		t.Fatalf("third put: %v", err)
	}
	if err := s.PutMetadata(ctx, userID, "main", []byte("meta-v4"), metadataHash([]byte("meta-v3"))); err != nil {
		t.Fatalf("fourth put: %v", err)
	}
	s.SetHistoryRetain(2)
	if err := s.PutMetadata(ctx, userID, "main", []byte("meta-v5"), metadataHash([]byte("meta-v4"))); err != nil {
		t.Fatalf("fifth put: %v", err)
	}
	h, _ = s.GetMetadataHistory(ctx, userID, "main", 10)
	if len(h) != 2 {
		t.Fatalf("history after prune = %d, want 2", len(h))
	}
	if string(h[0].Blob) != "meta-v4" || string(h[1].Blob) != "meta-v3" {
		t.Fatalf("pruned history = %q/%q, want v4/v3", h[0].Blob, h[1].Blob)
	}
}
