package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wii/senv/internal/server/store"
	"github.com/wii/senv/internal/server/testdb"
)

// setup 返回 HTTP server 与 alice/bob 两个用户的 token
func setup(t *testing.T) (*Server, string, string) {
	t.Helper()
	pool := testdb.New(t)
	st := store.New(pool)
	ctx := context.Background()
	aliceToken, err := st.CreateUser(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateUser alice: %v", err)
	}
	bobToken, err := st.CreateUser(ctx, "bob")
	if err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}
	return New(st), aliceToken, bobToken
}

func doRequest(t *testing.T, srv *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestHealthNoAuth(t *testing.T) {
	srv, _, _ := setup(t)
	rec := doRequest(t, srv, "GET", "/healthz", "", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("healthz = %d, want 200", rec.Code)
	}
}

func TestAuthRequired(t *testing.T) {
	srv, token, _ := setup(t)

	// 无 token → 401
	rec := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token = %d, want 401", rec.Code)
	}
	// 无效 token → 401（与缺失一致的响应语义）
	rec = doRequest(t, srv, "GET", "/v1/vaults/main/metadata", "bogus", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("bad token = %d, want 401", rec.Code)
	}
	// 有效 token → 非 401（vault 不存在 → 404）
	rec = doRequest(t, srv, "GET", "/v1/vaults/main/metadata", token, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("valid token missing vault = %d, want 404", rec.Code)
	}
}

func TestRevokedTokenRejected(t *testing.T) {
	pool := testdb.New(t)
	st := store.New(pool)
	token, err := st.CreateUser(context.Background(), "carol")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	srv := New(st)
	if err := st.RevokeToken(context.Background(), token); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	// 吊销后响应与 token 从未存在过一致
	rec := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", token, nil)
	bogus := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", "bogus", nil)
	if rec.Code != http.StatusUnauthorized || rec.Body.String() != bogus.Body.String() {
		t.Errorf("revoked token response should match unknown token: %d %q vs %d %q",
			rec.Code, rec.Body.String(), bogus.Code, bogus.Body.String())
	}
}

func TestMetadataHTTPRoundtrip(t *testing.T) {
	srv, token, _ := setup(t)
	blob := []byte("encrypted-metadata-blob")

	rec := doRequest(t, srv, "PUT", "/v1/vaults/main/metadata", token, metadataRequest{Blob: blob})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT metadata = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, srv, "GET", "/v1/vaults/main/metadata", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET metadata = %d: %s", rec.Code, rec.Body.String())
	}
	var resp metadataRequest
	json.NewDecoder(rec.Body).Decode(&resp)
	if !bytes.Equal(resp.Blob, blob) {
		t.Errorf("metadata roundtrip = %q, want %q", resp.Blob, blob)
	}
}

func TestCrossUserVaultNotFound(t *testing.T) {
	srv, aliceToken, bobToken := setup(t)

	// alice 创建 vault
	rec := doRequest(t, srv, "PUT", "/v1/vaults/main/metadata", aliceToken, metadataRequest{Blob: []byte("a")})
	if rec.Code != http.StatusOK {
		t.Fatalf("alice PUT metadata = %d", rec.Code)
	}
	// bob 访问同名 vault → 404，与不存在时响应一致
	recBob := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", bobToken, nil)
	recMissing := doRequest(t, srv, "GET", "/v1/vaults/never-existed/metadata", aliceToken, nil)
	if recBob.Code != http.StatusNotFound || recBob.Body.String() != recMissing.Body.String() {
		t.Errorf("cross-user access should be indistinguishable from missing vault: %d %q vs %d %q",
			recBob.Code, recBob.Body.String(), recMissing.Code, recMissing.Body.String())
	}
}

func TestPushConflictListsEntries(t *testing.T) {
	srv, token, _ := setup(t)

	push := func(entries []store.Entry) *httptest.ResponseRecorder {
		return doRequest(t, srv, "POST", "/v1/vaults/main/entries", token, pushRequest{Entries: entries})
	}

	// 初始推送
	rec := push([]store.Entry{{Kind: "env", Grp: "default", Key: "A", Ciphertext: []byte("ca"), BaseRevision: 0}})
	if rec.Code != http.StatusOK {
		t.Fatalf("initial push = %d: %s", rec.Code, rec.Body.String())
	}
	var okResp pushResponse
	json.NewDecoder(rec.Body).Decode(&okResp)
	if okResp.LatestRevision != 1 || len(okResp.Revisions) != 1 || okResp.Revisions[0].Revision != 1 {
		t.Errorf("push response = %+v, want A@1 latest=1", okResp)
	}

	// 过期 base_revision → 409 + 冲突清单
	rec = push([]store.Entry{{Kind: "env", Grp: "default", Key: "A", Ciphertext: []byte("stale"), BaseRevision: 0}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale push = %d, want 409", rec.Code)
	}
	var conflictBody struct {
		Conflicts []store.Conflict `json:"conflicts"`
	}
	json.NewDecoder(rec.Body).Decode(&conflictBody)
	if len(conflictBody.Conflicts) != 1 || conflictBody.Conflicts[0].Key != "A" || conflictBody.Conflicts[0].CurrentRevision != 1 {
		t.Errorf("409 conflicts = %+v, want A@1", conflictBody.Conflicts)
	}

	// 冲突后数据未被写入
	rec = doRequest(t, srv, "GET", "/v1/vaults/main/entries?since=0", token, nil)
	var pull pullResponse
	json.NewDecoder(rec.Body).Decode(&pull)
	if len(pull.Entries) != 1 || string(pull.Entries[0].Ciphertext) != "ca" {
		t.Errorf("entries after rejected push = %+v, want original A only", pull.Entries)
	}
}

func TestPullIncrementalWithDelete(t *testing.T) {
	srv, token, _ := setup(t)
	push := func(entries []store.Entry) {
		rec := doRequest(t, srv, "POST", "/v1/vaults/main/entries", token, pushRequest{Entries: entries})
		if rec.Code != http.StatusOK {
			t.Fatalf("push = %d: %s", rec.Code, rec.Body.String())
		}
	}
	push([]store.Entry{{Kind: "env", Grp: "default", Key: "A", Ciphertext: []byte("ca"), BaseRevision: 0}})
	push([]store.Entry{{Kind: "env", Grp: "default", Key: "A", BaseRevision: 1, Deleted: true}})

	// since=0 能看到最终删除状态；since=1 只看到删除标记条目
	rec := doRequest(t, srv, "GET", "/v1/vaults/main/entries?since=1", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("pull = %d", rec.Code)
	}
	var resp pullResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.Entries) != 1 || !resp.Entries[0].Deleted || resp.LatestRevision != 2 {
		t.Errorf("incremental pull = %+v, want 1 deleted entry, latest=2", resp)
	}

	// 空增量：since=latest → 空列表 + 相同 revision
	rec = doRequest(t, srv, "GET", "/v1/vaults/main/entries?since=2", token, nil)
	resp = pullResponse{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if rec.Code != http.StatusOK || len(resp.Entries) != 0 || resp.LatestRevision != 2 {
		t.Errorf("empty increment: code=%d entries=%d latest=%d, want 200/0/2", rec.Code, len(resp.Entries), resp.LatestRevision)
	}
}
