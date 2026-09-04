package provider

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeServer 是内存版 serverAPI，模拟 server 的乐观锁与 revision 语义
type fakeServer struct {
	metadata   map[string][]byte
	entries    map[string]map[string]Entry // vault -> id -> entry
	seq        map[string]int64
	fail       bool // 模拟断网
	failPush   bool
	beforePush func()
}

func newFakeServer() *fakeServer {
	return &fakeServer{
		metadata: make(map[string][]byte),
		entries:  make(map[string]map[string]Entry),
		seq:      make(map[string]int64),
	}
}

func (f *fakeServer) GetMetadata(_ context.Context, vault string) ([]byte, error) {
	if f.fail {
		return nil, errors.New("无法连接 server: connection refused")
	}
	blob, ok := f.metadata[vault]
	if !ok {
		return nil, ErrVaultNotFound
	}
	return blob, nil
}

func (f *fakeServer) PutMetadata(_ context.Context, vault string, blob []byte) error {
	if f.fail {
		return errors.New("无法连接 server: connection refused")
	}
	f.metadata[vault] = blob
	return nil
}

func (f *fakeServer) Push(_ context.Context, vault string, entries []Entry) ([]Entry, int64, error) {
	if f.beforePush != nil {
		f.beforePush()
	}
	if f.fail || f.failPush {
		return nil, 0, errors.New("无法连接 server: connection refused")
	}
	store := f.entries[vault]
	if store == nil {
		store = make(map[string]Entry)
		f.entries[vault] = store
	}
	// 乐观锁：任一冲突整批拒绝
	for _, e := range entries {
		cur, ok := store[entryID(e.Kind, e.Grp, e.Key)]
		curRev := int64(0)
		if ok {
			curRev = cur.Revision
		}
		if e.BaseRevision != curRev {
			deleted := !ok || cur.Deleted
			size := len(cur.Ciphertext)
			if deleted {
				size = 0
			}
			return nil, 0, &ConflictError{Conflicts: []Conflict{{
				Kind: e.Kind, Grp: e.Grp, Key: e.Key, CurrentRevision: curRev,
				Deleted: deleted, Size: int64(size), UpdatedAt: time.Now(),
			}}}
		}
	}
	for i := range entries {
		f.seq[vault]++
		entries[i].Revision = f.seq[vault]
		entries[i].UpdatedAt = time.Now()
		store[entryID(entries[i].Kind, entries[i].Grp, entries[i].Key)] = entries[i]
	}
	return entries, f.seq[vault], nil
}

func (f *fakeServer) Pull(_ context.Context, vault string, since int64) ([]Entry, int64, error) {
	if f.fail {
		return nil, 0, errors.New("无法连接 server: connection refused")
	}
	if _, ok := f.metadata[vault]; !ok {
		return nil, 0, ErrVaultNotFound
	}
	var out []Entry
	for _, e := range f.entries[vault] {
		if e.Revision > since {
			out = append(out, e)
		}
	}
	if out == nil {
		out = []Entry{}
	}
	return out, f.seq[vault], nil
}

// newTestProvider 在临时目录建立已 bootstrap 的 server provider
func newTestProvider(t *testing.T, srv *fakeServer) (*ServerProvider, *localCache) {
	t.Helper()
	configPath := t.TempDir()
	dataPath := t.TempDir()
	srv.metadata["main"] = []byte(`{"salt":"x","password_key":"y"}`)
	p := newServerProvider(srv, configPath, dataPath, "main")
	if err := p.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	return p, p.cache
}

func mustEntryPath(t testing.TB, cache *localCache, kind, grp, key string) string {
	t.Helper()
	path, err := cache.entryPath(kind, grp, key)
	if err != nil {
		t.Fatalf("entryPath(%q, %q, %q): %v", kind, grp, key, err)
	}
	return path
}

// writeEnvVar 在缓存中写入一个 env 条目文件
func writeEnvVar(t *testing.T, cache *localCache, grp, key, content string) {
	t.Helper()
	path := mustEntryPath(t, cache, KindEnv, grp, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSyncBidirectional(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	ctx := context.Background()

	// 远端有新条目（模拟另一台机器推送）
	srv.Push(ctx, "main", []Entry{{Kind: KindEnv, Grp: "default", Key: "REMOTE", Ciphertext: []byte("r"), BaseRevision: 0}})
	// 本地有新条目
	writeEnvVar(t, cache, "default", "LOCAL", "l")

	res, err := p.SyncWithReport(ctx)
	if err != nil {
		t.Fatalf("SyncWithReport: %v", err)
	}
	// 远端更改落盘
	if _, err := os.Stat(mustEntryPath(t, cache, KindEnv, "default", "REMOTE")); err != nil {
		t.Errorf("remote entry not applied to local cache: %v", err)
	}
	// 本地更改已推送
	if _, ok := srv.entries["main"][entryID(KindEnv, "default", "LOCAL")]; !ok {
		t.Error("local entry not pushed to server")
	}
	if res.Pull.Applied != 1 || res.Push.Pushed != 1 {
		t.Errorf("result = pull %d / push %d, want 1/1", res.Pull.Applied, res.Push.Pushed)
	}

	// 两端一致时：无待推送、无增量
	res, err = p.SyncWithReport(ctx)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if res.Dirty != 0 || res.Pull.Applied != 0 || res.Push.Pushed != 0 {
		t.Errorf("converged sync should be no-op: %+v", res)
	}
}

func TestSyncConflictKeepsBothSides(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	ctx := context.Background()

	// 初始条目经同步建立快照
	writeEnvVar(t, cache, "default", "A", "v1")
	if _, err := p.SyncWithReport(ctx); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	// 远端更新 A（另一台机器）；本地也修改 A → 冲突
	srv.Push(ctx, "main", []Entry{{Kind: KindEnv, Grp: "default", Key: "A", Ciphertext: []byte("remote-v2"), BaseRevision: 1}})
	writeEnvVar(t, cache, "default", "A", "local-v2")

	_, err := p.SyncWithReport(ctx)
	var conflictErr *SyncConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("sync should abort with SyncConflictError, got: %v", err)
	}
	if len(conflictErr.Conflicts) != 1 || conflictErr.Conflicts[0].Key != "A" {
		t.Errorf("conflicts = %+v, want entry A", conflictErr.Conflicts)
	}
	// 两端数据均未改动
	if got := srv.entries["main"][entryID(KindEnv, "default", "A")].Ciphertext; string(got) != "remote-v2" {
		t.Errorf("server entry changed to %q, want remote-v2", got)
	}
	data, _ := os.ReadFile(mustEntryPath(t, cache, KindEnv, "default", "A"))
	if string(data) != "local-v2" {
		t.Errorf("local entry changed to %q, want local-v2", data)
	}
	// 错误信息包含解决指引
	msg := err.Error()
	if !containsAll(msg, "--accept-remote", "--force-push") {
		t.Errorf("conflict error should include resolution guidance, got:\n%s", msg)
	}
	if len(conflictErr.Details) != 1 {
		t.Fatalf("conflict details = %+v, want one detail", conflictErr.Details)
	}
	detail := conflictErr.Details[0]
	if detail.Local.Revision != 1 || detail.Local.Deleted ||
		detail.Local.Size != len("local-v2") ||
		string(detail.Local.Ciphertext) != "local-v2" {
		t.Errorf("local conflict side = %+v, want base revision 1 and local-v2", detail.Local)
	}
	if detail.Remote.Revision != 2 || detail.Remote.Deleted ||
		detail.Remote.Size != len("remote-v2") || detail.Remote.UpdatedAt.IsZero() ||
		string(detail.Remote.Ciphertext) != "remote-v2" {
		t.Errorf("remote conflict side = %+v, want revision 2 and remote-v2", detail.Remote)
	}
	if strings.Contains(msg, "remote-v2") || strings.Contains(msg, "local-v2") {
		t.Errorf("conflict error must not contain ciphertext or plaintext: %s", msg)
	}
}

func TestSyncConflictDetailsCoverDeleteMetadataAndLegacyServer(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	ctx := context.Background()

	writeEnvVar(t, cache, "default", "A", "v1")
	writeEnvVar(t, cache, "default", "KEEP", "x")
	if _, err := p.SyncWithReport(ctx); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	// 初始推送的 revision 分配顺序不确定（map 迭代），必须读取真实值。
	st, err := p.cache.loadState()
	if err != nil {
		t.Fatal(err)
	}
	aRev := st.Entries[entryID(KindEnv, "default", "A")].Revision

	// 本地删除，远端也删除：两侧删除状态都必须进入 detail。
	srv.Push(ctx, "main", []Entry{{Kind: KindEnv, Grp: "default", Key: "A", BaseRevision: aRev, Deleted: true}})
	if err := os.Remove(mustEntryPath(t, cache, KindEnv, "default", "A")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache.metadataPath(), []byte("local-meta"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.metadata["main"] = []byte("remote-meta")

	_, err = p.SyncWithReport(ctx)
	var conflictErr *SyncConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("sync should abort with SyncConflictError, got: %v", err)
	}
	if len(conflictErr.Details) != 1 || !conflictErr.Details[0].Local.Deleted || !conflictErr.Details[0].Remote.Deleted {
		t.Errorf("delete conflict details = %+v, want both sides deleted", conflictErr.Details)
	}
	if conflictErr.Metadata == nil || string(conflictErr.Metadata.Local) != "local-meta" ||
		string(conflictErr.Metadata.Remote) != "remote-meta" {
		t.Errorf("metadata detail = %+v, want both blobs", conflictErr.Metadata)
	}
	if msg := err.Error(); strings.Contains(msg, "local-meta") || strings.Contains(msg, "remote-meta") {
		t.Errorf("metadata conflict error leaked blobs: %s", msg)
	}

	// 旧 server 缺少 deleted/size/updated_at 时，用匹配 pull 候选补齐描述符。
	candidate := Entry{
		Kind: KindText, Grp: "g", Key: "T", Revision: 7,
		Ciphertext: []byte("legacy"), UpdatedAt: time.Now(),
	}
	side := remoteConflictSide(Conflict{
		Kind: candidate.Kind, Grp: candidate.Grp, Key: candidate.Key,
		CurrentRevision: 7,
	}, candidate, true)
	if side.Deleted || side.Size != len("legacy") || side.UpdatedAt.IsZero() ||
		string(side.Ciphertext) != "legacy" {
		t.Errorf("legacy remote side = %+v, want candidate-backed descriptor", side)
	}
}

func TestConflictDetailsRefreshStaleRemoteCandidate(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	ctx := context.Background()

	writeEnvVar(t, cache, "default", "A", "v1")
	if _, err := p.SyncWithReport(ctx); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	srv.Push(ctx, "main", []Entry{{Kind: KindEnv, Grp: "default", Key: "A", Ciphertext: []byte("remote-v2"), BaseRevision: 1}})
	writeEnvVar(t, cache, "default", "A", "local-v2")
	pr, err := p.pull(ctx)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if len(pr.RemoteCandidates) != 1 || string(pr.RemoteCandidates[0].Ciphertext) != "remote-v2" {
		t.Fatalf("remote candidates = %+v, want remote-v2", pr.RemoteCandidates)
	}

	// 模拟 editor/网络窗口期间远端再次更新；409 revision 不再匹配 pull 候选。
	srv.Push(ctx, "main", []Entry{{Kind: KindEnv, Grp: "default", Key: "A", Ciphertext: []byte("remote-v3"), BaseRevision: 2}})
	conflict := Conflict{
		Kind: KindEnv, Grp: "default", Key: "A",
		CurrentRevision: 3, Size: int64(len("remote-v3")), UpdatedAt: time.Now(),
	}
	local := []Entry{{Kind: KindEnv, Grp: "default", Key: "A", BaseRevision: 1, Ciphertext: []byte("local-v2")}}
	got := p.buildConflictError(ctx, []Conflict{conflict}, false, local, pr.RemoteCandidates, nil, nil)
	if len(got.Details) != 1 || string(got.Details[0].Remote.Ciphertext) != "remote-v3" ||
		got.Details[0].Remote.Revision != 3 {
		t.Fatalf("refreshed details = %+v, want remote-v3@3", got.Details)
	}
}

func TestSyncOfflineAndRecover(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	ctx := context.Background()

	// 断网期间本地产生多次更改（读写不受限——写缓存本来就不需要网络）
	srv.fail = true
	writeEnvVar(t, cache, "default", "K1", "v1")
	writeEnvVar(t, cache, "default", "K2", "v2")
	if _, err := p.SyncWithReport(ctx); err == nil {
		t.Fatal("offline sync should report network failure")
	}
	// 本地数据不受影响
	if _, err := os.Stat(mustEntryPath(t, cache, KindEnv, "default", "K1")); err != nil {
		t.Errorf("local cache must survive offline sync failure: %v", err)
	}

	// 恢复网络：全部积压更改推送成功，状态收敛
	srv.fail = false
	if _, err := p.SyncWithReport(ctx); err != nil {
		t.Fatalf("sync after recover: %v", err)
	}
	for _, k := range []string{"K1", "K2"} {
		if _, ok := srv.entries["main"][entryID(KindEnv, "default", k)]; !ok {
			t.Errorf("backlog entry %s not pushed after recovery", k)
		}
	}
	res, _ := p.SyncWithReport(ctx)
	if res.Dirty != 0 {
		t.Errorf("state not converged after recovery, dirty=%d", res.Dirty)
	}
}

func TestSyncDeletePropagates(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	ctx := context.Background()

	writeEnvVar(t, cache, "default", "GONE", "x")
	if _, err := p.SyncWithReport(ctx); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	// 本地删除 → 推送删除标记
	os.Remove(mustEntryPath(t, cache, KindEnv, "default", "GONE"))
	if _, err := p.SyncWithReport(ctx); err != nil {
		t.Fatalf("sync delete: %v", err)
	}
	e := srv.entries["main"][entryID(KindEnv, "default", "GONE")]
	if !e.Deleted {
		t.Error("server entry should carry deleted flag")
	}

	// 远端删除 → 本地文件移除
	writeEnvVar(t, cache, "default", "GONE2", "y")
	p.SyncWithReport(ctx)
	srv.Push(ctx, "main", []Entry{{Kind: KindEnv, Grp: "default", Key: "GONE2", BaseRevision: e.Revision + 1, Deleted: true}})
	if _, err := p.SyncWithReport(ctx); err != nil {
		t.Fatalf("sync remote delete: %v", err)
	}
	if _, err := os.Stat(mustEntryPath(t, cache, KindEnv, "default", "GONE2")); !os.IsNotExist(err) {
		t.Error("locally cached file should be removed after remote delete")
	}
}

func TestAcceptRemoteAndForcePush(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	ctx := context.Background()

	writeEnvVar(t, cache, "default", "A", "v1")
	p.SyncWithReport(ctx)

	// 制造冲突
	srv.Push(ctx, "main", []Entry{{Kind: KindEnv, Grp: "default", Key: "A", Ciphertext: []byte("remote-v2"), BaseRevision: 1}})
	writeEnvVar(t, cache, "default", "A", "local-v2")
	if _, err := p.SyncWithReport(ctx); err == nil {
		t.Fatal("expected conflict")
	}

	// accept-remote：本地以远端为准
	if err := p.AcceptRemote(ctx); err != nil {
		t.Fatalf("AcceptRemote: %v", err)
	}
	data, _ := os.ReadFile(mustEntryPath(t, cache, KindEnv, "default", "A"))
	if string(data) != "remote-v2" {
		t.Errorf("after accept-remote local = %q, want remote-v2", data)
	}

	// 再造冲突，force-push：远端以本地为准
	srv.Push(ctx, "main", []Entry{{Kind: KindEnv, Grp: "default", Key: "A", Ciphertext: []byte("remote-v3"), BaseRevision: 2}})
	writeEnvVar(t, cache, "default", "A", "local-v3")
	if _, err := p.SyncWithReport(ctx); err == nil {
		t.Fatal("expected conflict")
	}
	if err := p.ForcePush(ctx); err != nil {
		t.Fatalf("ForcePush: %v", err)
	}
	if got := srv.entries["main"][entryID(KindEnv, "default", "A")].Ciphertext; string(got) != "local-v3" {
		t.Errorf("after force-push server = %q, want local-v3", got)
	}
	// 状态收敛
	if _, err := p.SyncWithReport(ctx); err != nil {
		t.Fatalf("sync after force-push should converge: %v", err)
	}
}

func TestBootstrapRequiresExistingVault(t *testing.T) {
	srv := newFakeServer()
	p := newServerProvider(srv, t.TempDir(), t.TempDir(), "missing")
	if err := p.Bootstrap(context.Background()); !errors.Is(err, ErrVaultNotFound) {
		t.Errorf("Bootstrap on missing vault = %v, want ErrVaultNotFound", err)
	}
}

func TestStatusReportsDirty(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	writeEnvVar(t, cache, "default", "PENDING", "x")
	status, err := p.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !containsAll(status, "待推送条目: 1", "PENDING") {
		t.Errorf("status should report dirty entry, got:\n%s", status)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
