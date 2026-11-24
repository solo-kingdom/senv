package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"
)

// metadataTestHash 与 server 端 metadataHash 同口径（sha256 十六进制）
func metadataTestHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// metadata 乐观锁：base_hash 过期返回 409，不静默覆盖。
func TestPutMetadataBaseHashConflict(t *testing.T) {
	st := newFakeStore()
	_, token := st.addClientToken("alice", "laptop", "active")
	uid := st.users["alice"]
	s := New(st)

	hashV1 := func() string { return metadataTestHash(st.getMetadata(uid, "main")) }

	if rec := doRequest(t, s, "PUT", "/v1/vaults/main/metadata", token, map[string]any{
		"blob": []byte("meta-v1"),
	}); rec.Code != http.StatusOK {
		t.Fatalf("first put = %d: %s", rec.Code, rec.Body.String())
	}
	base := hashV1()
	if rec := doRequest(t, s, "PUT", "/v1/vaults/main/metadata", token, map[string]any{
		"blob": []byte("meta-v2"), "base_hash": base,
	}); rec.Code != http.StatusOK {
		t.Fatalf("second put = %d: %s", rec.Code, rec.Body.String())
	}
	// 用过期 base 再推 → 409
	if rec := doRequest(t, s, "PUT", "/v1/vaults/main/metadata", token, map[string]any{
		"blob": []byte("meta-v3"), "base_hash": base,
	}); rec.Code != http.StatusConflict {
		t.Fatalf("stale base put = %d, want 409", rec.Code)
	}
	// 内容未被覆盖
	if got := st.getMetadata(uid, "main"); string(got) != "meta-v2" {
		t.Errorf("metadata = %q, want meta-v2", got)
	}
}
