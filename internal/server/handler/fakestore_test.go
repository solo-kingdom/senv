// fakeStore 是 store.Store 的内存实现，仅服务 handler 的 HTTP 层测试：
// 让请求-响应断言不再依赖 testcontainers。它只模拟 HTTP 层可观察的语义
// （401/403/404/409 与 JSON 形状）；事务、冲突收集、revision 连续性等
// SQL 行为仍由 store 包的 testcontainers 集成测试守住，这里不做复制。
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wii/senv/internal/server/store"
)

type fakeEntryKey struct {
	vaultID int64
	kind    string
	grp     string
	key     string
}

type fakeVaultKey struct {
	userID int64
	name   string
}

type fakeStore struct {
	mu sync.Mutex

	nextUserID   int64
	nextClientID int64
	nextVaultID  int64

	users    map[string]int64            // 用户名 -> id
	tokens   map[string]store.AuthResult // 明文 token -> 认证结果（仅测试内存；键为明文只为构造方便）
	regCodes map[int64][]string          // userID -> 未用注册码
	nextCode int
	revoked  map[string]bool
	clients  map[int64]*store.Client
	metadata map[int64][]byte
	vaults   map[fakeVaultKey]int64
	seq      map[int64]int64
	entries  map[fakeEntryKey]store.Entry
	touches  []int64
	access   []store.AccessEvent
	history  []store.HistoryVersion
	retain   int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		users:    map[string]int64{},
		tokens:   map[string]store.AuthResult{},
		regCodes: map[int64][]string{},
		revoked:  map[string]bool{},
		clients:  map[int64]*store.Client{},
		metadata: map[int64][]byte{},
		vaults:   map[fakeVaultKey]int64{},
		seq:      map[int64]int64{},
		entries:  map[fakeEntryKey]store.Entry{},
		retain:   store.DefaultHistoryRetain,
	}
}

// addUser / addClientToken / revoke 是测试装配 helper：直接铺设认证场景
func (f *fakeStore) addUser(name string) (int64, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextUserID++
	id := f.nextUserID
	f.users[name] = id
	token := fmt.Sprintf("token-%s-%d", name, id)
	f.tokens[token] = store.AuthResult{UserID: id}
	return id, token
}

func (f *fakeStore) addClientToken(userName, clientName, status string) (int64, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextUserID++
	userID := f.nextUserID
	f.users[userName] = userID
	f.nextClientID++
	id := f.nextClientID
	f.clients[id] = &store.Client{ID: id, UserID: userID, Name: clientName, Status: status}
	token := fmt.Sprintf("ctoken-%s-%d", clientName, id)
	f.tokens[token] = store.AuthResult{UserID: userID, ClientID: id, ClientStatus: status, ClientBlocked: status == store.ClientStatusBlocked}
	return id, token
}

func (f *fakeStore) setBlocked(clientID int64, blocked bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.clients[clientID]
	status := store.ClientStatusActive
	if blocked {
		status = store.ClientStatusBlocked
	}
	c.Status = status
	for t, res := range f.tokens {
		if res.ClientID == clientID {
			res.ClientStatus = status
			res.ClientBlocked = blocked
			f.tokens[t] = res
		}
	}
}

// fakeStore 编译期即跟随 Store 接口：接口增删方法时此断言先于测试失败
var _ store.Store = (*fakeStore)(nil)

func (f *fakeStore) Close() {}

// Pool 返回 nil：fake 不承载真实连接池，handler 请求路径不得触碰该入口
func (f *fakeStore) Pool() *pgxpool.Pool {
	return nil
}

func (f *fakeStore) CreateUser(_ context.Context, name string) (string, error) {
	_, token := f.addUser(name)
	return token, nil
}

func (f *fakeStore) RevokeToken(_ context.Context, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.tokens[token]; !ok || f.revoked[token] {
		return fmt.Errorf("token 不存在或已吊销")
	}
	f.revoked[token] = true
	return nil
}

func (f *fakeStore) Authenticate(_ context.Context, token string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	res, ok := f.tokens[token]
	if !ok || f.revoked[token] {
		return 0, store.ErrNotFound
	}
	return res.UserID, nil
}

func (f *fakeStore) AuthenticateWithClient(_ context.Context, token string) (store.AuthResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	res, ok := f.tokens[token]
	if !ok || f.revoked[token] {
		return store.AuthResult{}, store.ErrNotFound
	}
	return res, nil
}

func (f *fakeStore) GetMetadata(_ context.Context, userID int64, vault string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.vaults[fakeVaultKey{userID, vault}]
	if !ok {
		return nil, store.ErrNotFound
	}
	blob, ok := f.metadata[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return blob, nil
}

func (f *fakeStore) PutMetadata(_ context.Context, userID int64, vault string, blob []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fakeVaultKey{userID, vault}
	id, ok := f.vaults[key]
	if !ok {
		f.nextVaultID++
		id = f.nextVaultID
		f.vaults[key] = id
	}
	f.metadata[id] = append([]byte(nil), blob...)
	return nil
}

func (f *fakeStore) vaultID(userID int64, vault string, create bool) (int64, error) {
	key := fakeVaultKey{userID, vault}
	id, ok := f.vaults[key]
	if !ok {
		if !create {
			return 0, store.ErrNotFound
		}
		f.nextVaultID++
		id = f.nextVaultID
		f.vaults[key] = id
	}
	return id, nil
}

func (f *fakeStore) PushEntries(_ context.Context, userID int64, vault string, entries []store.Entry) ([]store.Entry, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, err := f.vaultID(userID, vault, true)
	if err != nil {
		return nil, 0, err
	}
	// 冲突语义只保留 HTTP 层可观察的最小集：base_revision 与当前不符 → 409
	var conflicts []store.Conflict
	for _, e := range entries {
		k := fakeEntryKey{id, e.Kind, e.Grp, e.Key}
		cur, exists := f.entries[k]
		var currentRev int64
		if exists {
			currentRev = cur.Revision
		}
		if e.BaseRevision != currentRev {
			conflicts = append(conflicts, store.Conflict{Kind: e.Kind, Grp: e.Grp, Key: e.Key, CurrentRevision: currentRev, Deleted: exists && cur.Deleted})
		}
	}
	if len(conflicts) > 0 {
		return nil, 0, &store.ConflictError{Conflicts: conflicts}
	}
	var latest int64
	for i := range entries {
		f.seq[id]++
		rev := f.seq[id]
		entries[i].Revision = rev
		latest = rev
		k := fakeEntryKey{id, entries[i].Kind, entries[i].Grp, entries[i].Key}
		f.entries[k] = entries[i]
	}
	return entries, latest, nil
}

func (f *fakeStore) PullEntries(_ context.Context, userID int64, vault string, since int64) ([]store.Entry, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, err := f.vaultID(userID, vault, false)
	if err != nil {
		return nil, 0, err
	}
	out := []store.Entry{}
	for k, e := range f.entries {
		if k.vaultID == id && e.Revision > since {
			out = append(out, e)
		}
	}
	// HTTP 层只断言形状与 404/200，不依赖顺序（SQL 语义由集成测试守住）
	return out, f.seq[id], nil
}

func (f *fakeStore) CreateRegistrationCode(_ context.Context, userID int64, _ time.Duration) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextCode++
	code := fmt.Sprintf("code-%d", f.nextCode)
	f.regCodes[userID] = append(f.regCodes[userID], code)
	return code, nil
}

func (f *fakeStore) RegisterClient(_ context.Context, code, name string) (string, *store.Client, error) {
	f.mu.Lock()
	for uid, codes := range f.regCodes {
		for i, c := range codes {
			if c == code {
				f.regCodes[uid] = append(codes[:i], codes[i+1:]...)
				f.mu.Unlock()
				userName := ""
				for n, id := range f.users {
					if id == uid {
						userName = n
					}
				}
				clientID, token := f.addClientToken(userName, name, store.ClientStatusActive)
				return token, &store.Client{ID: clientID, UserID: uid, Name: name, Status: store.ClientStatusActive}, nil
			}
		}
	}
	f.mu.Unlock()
	return "", nil, store.ErrNotFound
}

func (f *fakeStore) SetClientStatus(_ context.Context, _ int64, _ string, _ string) error {
	return errors.New("fakeStore: not implemented")
}

func (f *fakeStore) ListClients(_ context.Context, _ int64) ([]store.Client, error) {
	return nil, errors.New("fakeStore: not implemented")
}

func (f *fakeStore) UserIDByName(_ context.Context, name string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.users[name]
	if !ok {
		return 0, store.ErrNotFound
	}
	return id, nil
}

func (f *fakeStore) UserIDByToken(_ context.Context, token string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.revoked[token] {
		return 0, store.ErrNotFound
	}
	res, ok := f.tokens[token]
	if !ok {
		return 0, store.ErrNotFound
	}
	return res.UserID, nil
}

func (f *fakeStore) TouchClient(_ context.Context, clientID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.touches = append(f.touches, clientID)
}

func (f *fakeStore) RecordAccess(_ context.Context, e store.AccessEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.access = append(f.access, e)
	return nil
}

func (f *fakeStore) ListAccessLogs(_ context.Context, _ store.AccessLogFilter) ([]store.AccessEventRow, error) {
	return nil, errors.New("fakeStore: not implemented")
}

func (f *fakeStore) PruneAccessLogs(_ context.Context, _ time.Time) (int64, error) {
	return 0, errors.New("fakeStore: not implemented")
}

func (f *fakeStore) SetHistoryRetain(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retain = n
}

func (f *fakeStore) ListHistory(_ context.Context, userID int64, vault string, _ store.HistoryFilter) ([]store.HistoryVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.vaults[fakeVaultKey{userID, vault}]; !ok {
		return nil, store.ErrNotFound
	}
	return append([]store.HistoryVersion(nil), f.history...), nil
}

// preloadHistory 为 history 断言铺设数据（fake 不复算 SQL 留存规则）
func (f *fakeStore) preloadHistory(userID int64, vault string, versions ...store.HistoryVersion) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fakeVaultKey{userID, vault}
	if _, ok := f.vaults[key]; !ok {
		f.nextVaultID++
		f.vaults[key] = f.nextVaultID
	}
	f.history = append(f.history, versions...)
}

func newFakeServer(st *fakeStore) *Server {
	// 关闭限速（负值）：fake 测试不耦合限速行为，限速语义由 ratelimit 测试守住
	return New(st, Options{AuthRateLimit: -1})
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v (%q)", err, rec.Body.String())
	}
	return body.Error
}

func TestFakeAuthMissingToken(t *testing.T) {
	srv := newFakeServer(newFakeStore())
	rec := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("missing token = %d, want 401", rec.Code)
	}
}

func TestFakeAuthUnknownAndRevokedToken(t *testing.T) {
	f := newFakeStore()
	srv := newFakeServer(f)
	if rec := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", "no-such-token", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("unknown token = %d, want 401", rec.Code)
	}
	_, token := f.addUser("alice")
	if err := f.RevokeToken(context.Background(), token); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	rec := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", token, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("revoked token = %d, want 401", rec.Code)
	}
	if got := decodeError(t, rec); got != "unauthorized" {
		t.Errorf("error = %q, want unauthorized（不泄露存在性）", got)
	}
}

func TestFakeAuthBlockedClient(t *testing.T) {
	f := newFakeStore()
	srv := newFakeServer(f)
	clientID, token := f.addClientToken("alice", "laptop", store.ClientStatusActive)
	// 屏蔽前可正常访问
	rec := doRequest(t, srv, "PUT", "/v1/vaults/main/metadata", token, map[string]any{"blob": "aGk="})
	if rec.Code != http.StatusOK {
		t.Fatalf("pre-block put = %d, want 200", rec.Code)
	}
	// 屏蔽后 403 + client_blocked，与 401 可区分
	f.setBlocked(clientID, true)
	rec = doRequest(t, srv, "GET", "/v1/vaults/main/metadata", token, nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("blocked = %d, want 403", rec.Code)
	}
	if got := decodeError(t, rec); got != "client_blocked" {
		t.Errorf("error = %q, want client_blocked", got)
	}
	// 解封恢复
	f.setBlocked(clientID, false)
	if rec := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", token, nil); rec.Code != http.StatusOK {
		t.Errorf("unblocked = %d, want 200", rec.Code)
	}
}

func TestFakeAuthTouchesClient(t *testing.T) {
	f := newFakeStore()
	srv := newFakeServer(f)
	clientID, token := f.addClientToken("alice", "laptop", store.ClientStatusActive)
	// 受保护端点通过认证后应 best-effort 刷新 last_seen（healthz 不过认证，不算）
	doRequest(t, srv, "GET", "/v1/vaults/main/entries?since=0", token, nil)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.touches) == 0 || f.touches[0] != clientID {
		t.Errorf("touches = %v, want [%d]", f.touches, clientID)
	}
}

func TestFakeMetadataRoundtrip(t *testing.T) {
	f := newFakeStore()
	srv := newFakeServer(f)
	_, token := f.addUser("alice")
	if rec := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", token, nil); rec.Code != http.StatusNotFound {
		t.Errorf("get before put = %d, want 404", rec.Code)
	}
	if rec := doRequest(t, srv, "PUT", "/v1/vaults/main/metadata", token, map[string]any{"blob": "c2FsdA=="}); rec.Code != http.StatusOK {
		t.Fatalf("put = %d, want 200", rec.Code)
	}
	rec := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d, want 200", rec.Code)
	}
	var resp struct {
		Blob []byte `json:"blob"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(resp.Blob) != "salt" {
		t.Errorf("blob = %q, want salt", resp.Blob)
	}
}

func TestFakePushPullAndConflict(t *testing.T) {
	f := newFakeStore()
	srv := newFakeServer(f)
	_, token := f.addUser("alice")

	entry := func(base int64) map[string]any {
		return map[string]any{
			"kind": "env", "grp": "default", "key": "K",
			"ciphertext": "Y2lwaGVydGV4dA==", "base_revision": base,
		}
	}
	// 首推（base 0）成功，ack revision 1
	rec := doRequest(t, srv, "POST", "/v1/vaults/main/entries", token, map[string]any{"entries": []any{entry(0)}})
	if rec.Code != http.StatusOK {
		t.Fatalf("first push = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var push struct {
		Revisions []struct {
			Revision int64 `json:"revision"`
		} `json:"revisions"`
		LatestRevision int64 `json:"latest_revision"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &push); err != nil {
		t.Fatalf("decode push resp: %v", err)
	}
	if len(push.Revisions) != 1 || push.Revisions[0].Revision != 1 || push.LatestRevision != 1 {
		t.Fatalf("push ack = %+v, want revision 1", push)
	}
	// 过期 base 再推 → 409，冲突体带 server 端当前 revision
	rec = doRequest(t, srv, "POST", "/v1/vaults/main/entries", token, map[string]any{"entries": []any{entry(0)}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale push = %d, want 409", rec.Code)
	}
	var conflict struct {
		Conflicts []store.Conflict `json:"conflicts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &conflict); err != nil {
		t.Fatalf("decode conflict: %v", err)
	}
	if len(conflict.Conflicts) != 1 || conflict.Conflicts[0].CurrentRevision != 1 {
		t.Fatalf("conflicts = %+v, want current_revision 1", conflict.Conflicts)
	}
	// 匹配 base 再推 → 成功 revision 2；pull 全量返回
	if rec := doRequest(t, srv, "POST", "/v1/vaults/main/entries", token, map[string]any{"entries": []any{entry(1)}}); rec.Code != http.StatusOK {
		t.Fatalf("matching push = %d, want 200", rec.Code)
	}
	rec = doRequest(t, srv, "GET", "/v1/vaults/main/entries?since=0", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("pull = %d, want 200", rec.Code)
	}
	var pull struct {
		Entries        []store.Entry `json:"entries"`
		LatestRevision int64         `json:"latest_revision"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pull); err != nil {
		t.Fatalf("decode pull: %v", err)
	}
	if len(pull.Entries) != 1 || pull.Entries[0].Revision != 2 || pull.LatestRevision != 2 {
		t.Fatalf("pull = %+v, want revision 2 entry", pull)
	}
}

func TestFakePullMissingVaultAndBadSince(t *testing.T) {
	f := newFakeStore()
	srv := newFakeServer(f)
	_, token := f.addUser("alice")
	if rec := doRequest(t, srv, "GET", "/v1/vaults/none/entries?since=0", token, nil); rec.Code != http.StatusNotFound {
		t.Errorf("missing vault pull = %d, want 404", rec.Code)
	}
	if rec := doRequest(t, srv, "GET", "/v1/vaults/main/entries?since=x", token, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad since = %d, want 400", rec.Code)
	}
	if rec := doRequest(t, srv, "GET", "/v1/vaults/main/entries?since=-1", token, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("negative since = %d, want 400", rec.Code)
	}
}

func TestFakeHistoryShape(t *testing.T) {
	f := newFakeStore()
	srv := newFakeServer(f)
	userID, token := f.addUser("alice")
	f.preloadHistory(userID, "main", store.HistoryVersion{
		Kind: "env", Grp: "default", Key: "K", Revision: 1, Ciphertext: []byte("old"),
	})
	rec := doRequest(t, srv, "GET", "/v1/vaults/main/history", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("history = %d, want 200", rec.Code)
	}
	var resp struct {
		History []store.HistoryVersion `json:"history"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(resp.History) != 1 || resp.History[0].Key != "K" {
		t.Fatalf("history = %+v, want 1 entry for K", resp.History)
	}
	if rec := doRequest(t, srv, "GET", "/v1/vaults/none/history", token, nil); rec.Code != http.StatusNotFound {
		t.Errorf("missing vault history = %d, want 404", rec.Code)
	}
	if rec := doRequest(t, srv, "GET", "/v1/vaults/main/history?limit=0", token, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad limit = %d, want 400", rec.Code)
	}
}
