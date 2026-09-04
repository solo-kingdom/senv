package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// rewriteStateFile 直接改写磁盘状态文件（绕过护栏），用于构造损坏/历史形态。
func rewriteStateFile(t *testing.T, cache *localCache, mutate func(*syncState)) {
	t.Helper()
	st, ok, err := cache.readStateRaw()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("state file missing before rewrite")
	}
	mutate(st)
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache.dataPath, syncStateFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readStateFile(t *testing.T, cache *localCache) *syncState {
	t.Helper()
	st, ok, err := cache.readStateRaw()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("state file missing")
	}
	return st
}

// --- 1.1 绑定与来源字段 ---

func TestServerFingerprintStableAndTruncated(t *testing.T) {
	a := serverFingerprint("http://10.0.0.5:8080")
	if a != serverFingerprint("http://10.0.0.5:8080/") {
		t.Fatalf("trailing slash changes fingerprint: %q vs %q", a, serverFingerprint("http://10.0.0.5:8080/"))
	}
	if len(a) != 16 || strings.ContainsAny(a, "ghijklmnopqrstuvwxyz/:") {
		t.Fatalf("fingerprint = %q, want 16 lowercase hex chars", a)
	}
}

func TestNewServerProviderBindsVaultAndStampsFields(t *testing.T) {
	p := NewServerProvider("http://example.com", "tok", t.TempDir(), t.TempDir(), "main")
	if p.cache.binding == nil {
		t.Fatal("production provider must carry vault binding")
	}
	if p.cache.binding.Server != serverFingerprint("http://example.com") || p.cache.binding.Vault != "main" {
		t.Fatalf("binding = %+v", p.cache.binding)
	}
	if err := p.cache.saveStateOpts(newSyncState(), stateWriteOptions{writerPath: "test"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(p.cache.dataPath, syncStateFileName))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["vault_binding"]; !ok {
		t.Error("state JSON missing vault_binding")
	}
	writtenBy, ok := fields["written_by"].(map[string]any)
	if !ok {
		t.Fatal("state JSON missing written_by")
	}
	if writtenBy["path"] != "test" || writtenBy["pid"].(float64) <= 0 || writtenBy["ts"].(float64) <= 0 {
		t.Errorf("written_by = %+v", writtenBy)
	}
}

// --- 1.2 防退化护栏 ---

func seedStateFile(t *testing.T, cache *localCache, entries int, metadataHash string) {
	t.Helper()
	st := newSyncState()
	st.MetadataHash = metadataHash
	for i := 0; i < entries; i++ {
		st.Entries[fmt.Sprintf("env\x00g\x00K%d", i)] = syncEntryState{Revision: int64(i + 1), Hash: fmt.Sprintf("h%d", i)}
	}
	data, _ := json.MarshalIndent(st, "", "  ")
	if err := os.WriteFile(filepath.Join(cache.dataPath, syncStateFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSaveStateRejectsSnapshotShrinkWithoutTombstone(t *testing.T) {
	cache := &localCache{configPath: t.TempDir(), dataPath: t.TempDir()}
	seedStateFile(t, cache, 2, "abc")
	next := newSyncState()
	next.MetadataHash = "abc"
	next.Entries["env\x00g\x00K0"] = syncEntryState{Revision: 1, Hash: "h0"}
	err := cache.saveStateOpts(next, stateWriteOptions{writerPath: "test"})
	var regression *StateRegressionError
	if !errors.As(err, &regression) || regression.LostEntries != 1 {
		t.Fatalf("err = %v, want StateRegressionError(1 lost)", err)
	}
	if readStateFile(t, cache).MetadataHash != "abc" || len(readStateFile(t, cache).Entries) != 2 {
		t.Fatal("rejected write must keep existing file")
	}
}

func TestSaveStateRejectsMetadataHashClear(t *testing.T) {
	cache := &localCache{configPath: t.TempDir(), dataPath: t.TempDir()}
	seedStateFile(t, cache, 1, "abc")
	next := newSyncState()
	next.Entries["env\x00g\x00K0"] = syncEntryState{Revision: 1, Hash: "h0"}
	err := cache.saveStateOpts(next, stateWriteOptions{writerPath: "test"})
	var regression *StateRegressionError
	if !errors.As(err, &regression) || !regression.MetadataCleared {
		t.Fatalf("err = %v, want StateRegressionError(metadata cleared)", err)
	}
}

func TestApplyRemoteRejectsSnapshotShrink(t *testing.T) {
	cache := &localCache{configPath: t.TempDir(), dataPath: t.TempDir()}
	seedStateFile(t, cache, 2, "abc")
	next := newSyncState()
	next.MetadataHash = "abc"
	err := cache.applyRemoteOpts(nil, nil, false, next, stateWriteOptions{writerPath: "test"})
	var regression *StateRegressionError
	if !errors.As(err, &regression) {
		t.Fatalf("err = %v, want StateRegressionError", err)
	}
	if len(readStateFile(t, cache).Entries) != 2 {
		t.Fatal("rejected applyRemote must keep existing entries")
	}
}

type noMetadataServer struct{ serverAPI }

func (n noMetadataServer) GetMetadata(context.Context, string) ([]byte, error) {
	return nil, ErrVaultNotFound
}

func TestBootstrapAndAcceptRemoteAllowLegitimateRebuild(t *testing.T) {
	srv := newFakeServer()
	srv.metadata["main"] = []byte(`{"salt":"x","password_key":"y"}`)
	p := newServerProvider(srv, t.TempDir(), t.TempDir(), "main")
	if err := p.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 重建路径允许 entries 收缩：预置一个多余快照后重新 bootstrap。
	rewriteStateFile(t, p.cache, func(st *syncState) {
		st.Entries["env\x00g\x00ghost"] = syncEntryState{Revision: 9, Hash: "ghost"}
	})
	if err := p.bootstrapLocked(context.Background()); err != nil {
		t.Fatalf("bootstrap rebuild: %v", err)
	}
	if _, ok := readStateFile(t, p.cache).Entries["env\x00g\x00ghost"]; ok {
		t.Fatal("rebuild should replace snapshot wholesale")
	}

	// acceptRemote 在远端 metadata 缺失（404）时允许空哈希（远端确无 metadata）。
	p.api = noMetadataServer{srv}
	if err := p.acceptRemoteLocked(context.Background()); err != nil {
		t.Fatalf("acceptRemote without remote metadata: %v", err)
	}
	// 空哈希中间态未被护栏拒绝；随后 pushLocked 会把本地 metadata 上传并回填哈希。
}

// --- 1.3 vault 绑定校验 ---

func TestLoadStateRejectsForeignVaultBinding(t *testing.T) {
	cache := &localCache{
		configPath: t.TempDir(),
		dataPath:   t.TempDir(),
		binding:    &vaultBinding{Server: "aaaaaaaaaaaaaaaa", Vault: "expected"},
	}
	seedStateFile(t, cache, 0, "abc")
	rewriteStateFile(t, cache, func(st *syncState) {
		st.VaultBinding = &vaultBinding{Server: "bbbbbbbbbbbbbbbb", Vault: "other"}
	})
	_, err := cache.loadState()
	if err == nil || !strings.Contains(err.Error(), "accept-remote") {
		t.Fatalf("err = %v, want binding mismatch with rebuild hint", err)
	}
}

func TestLegacyStateGainsBindingOnSuccessfulWrite(t *testing.T) {
	srv := newFakeServer()
	srv.metadata["main"] = []byte(`{"salt":"x","password_key":"y"}`)
	p := newServerProviderWithBinding(srv, t.TempDir(), t.TempDir(), "main",
		vaultBinding{Server: "cccccccccccccccc", Vault: "main"})
	if err := p.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 抹掉绑定模拟旧版文件，之后一次成功同步应补全。
	rewriteStateFile(t, p.cache, func(st *syncState) { st.VaultBinding = nil })
	if _, err := p.pull(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := readStateFile(t, p.cache)
	if st.VaultBinding == nil || st.VaultBinding.Server != "cccccccccccccccc" {
		t.Fatalf("binding not restamped: %+v", st.VaultBinding)
	}
}

// --- 2.1 / 2.3 metadata 假冲突自愈 ---

func TestPullHealsMetadataSnapshotWhenBytesEqual(t *testing.T) {
	p, cache := newTestProvider(t, newFakeServer())
	rewriteStateFile(t, cache, func(st *syncState) { st.MetadataHash = "" })
	res, err := p.pull(context.Background())
	if err != nil {
		t.Fatalf("pull should heal instead of conflict: %v", err)
	}
	if !res.MetadataHealed {
		t.Errorf("MetadataHealed = false, want true")
	}
	if readStateFile(t, cache).MetadataHash == "" {
		t.Error("snapshot metadata hash still empty after heal")
	}
}

// --- 2.2 push 409 假冲突自愈 ---

func corruptSnapshotFor(t *testing.T, cache *localCache, kind, grp, key string) {
	t.Helper()
	id := entryID(kind, grp, key)
	rewriteStateFile(t, cache, func(st *syncState) {
		delete(st.Entries, id)
		st.MetadataHash = ""
	})
}

func TestPushHealsMissingSnapshotWhenBytesEqual(t *testing.T) {
	p, cache := newTestProvider(t, newFakeServer())
	writeEnvVar(t, cache, "default", "E", "value")
	if _, err := p.AutoPush(context.Background(), autoSyncBudget); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(mustEntryPath(t, cache, KindEnv, "default", "E"))
	if err != nil {
		t.Fatal(err)
	}
	corruptSnapshotFor(t, cache, KindEnv, "default", "E")

	res, err := p.SyncWithReport(context.Background())
	if err != nil {
		t.Fatalf("sync should heal false conflict: %v", err)
	}
	if res.Healed != 2 { // 条目快照 + metadata 哈希
		t.Errorf("Healed = %d, want 2", res.Healed)
	}
	after, _ := os.ReadFile(mustEntryPath(t, cache, KindEnv, "default", "E"))
	if string(before) != string(after) {
		t.Error("heal must not change data bytes")
	}
	st := readStateFile(t, cache)
	snap, ok := st.Entries[entryID(KindEnv, "default", "E")]
	if !ok || snap.Revision == 0 {
		t.Fatalf("snapshot not adopted: %+v", snap)
	}
	if st.MetadataHash == "" {
		t.Error("metadata hash not adopted")
	}
}

func TestPushConflictWhenBytesDiffer(t *testing.T) {
	p, cache := newTestProvider(t, newFakeServer())
	writeEnvVar(t, cache, "default", "E", "local")
	if _, err := p.AutoPush(context.Background(), autoSyncBudget); err != nil {
		t.Fatal(err)
	}
	// 第二台机器推送新版本，制造真实差异。
	b := newServerProvider(newFakeServer(), t.TempDir(), t.TempDir(), "unused")
	_ = b
	machineB := newServerProvider(p.api, t.TempDir(), t.TempDir(), "main")
	srv := p.api.(*fakeServer)
	_ = srv
	machineB.cache = &localCache{configPath: t.TempDir(), dataPath: t.TempDir()}
	if err := machineB.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	writeEnvVar(t, machineB.cache, "default", "E", "remote-new")
	if _, err := machineB.AutoPush(context.Background(), autoSyncBudget); err != nil {
		t.Fatal(err)
	}
	// 本地也改成与远端不同的内容，并抹掉快照 → 409 且不可自愈。
	writeEnvVar(t, cache, "default", "E", "local-new")
	corruptSnapshotFor(t, cache, KindEnv, "default", "E")
	_, err := p.SyncWithReport(context.Background())
	var conflict *SyncConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want SyncConflictError", err)
	}
	if len(conflict.Conflicts) == 0 {
		t.Error("want at least one entry conflict")
	}
	if _, ok := readStateFile(t, cache).Entries[entryID(KindEnv, "default", "E")]; ok {
		t.Error("differing bytes must not be adopted")
	}
}

func TestPushDoesNotHealDeletionConflict(t *testing.T) {
	p, cache := newTestProvider(t, newFakeServer())
	writeEnvVar(t, cache, "default", "E", "v1")
	if _, err := p.AutoPush(context.Background(), autoSyncBudget); err != nil {
		t.Fatal(err)
	}
	machineB := newServerProvider(p.api, t.TempDir(), t.TempDir(), "main")
	if err := machineB.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	writeEnvVar(t, machineB.cache, "default", "E", "v2")
	if _, err := machineB.AutoPush(context.Background(), autoSyncBudget); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(mustEntryPath(t, cache, KindEnv, "default", "E")); err != nil {
		t.Fatal(err)
	}
	_, err := p.SyncWithReport(context.Background())
	var conflict *SyncConflictError
	if !errors.As(err, &conflict) || len(conflict.Conflicts) == 0 {
		t.Fatalf("err = %v, want deletion SyncConflictError", err)
	}
}

type pullFailingServer struct {
	serverAPI
	mu        sync.Mutex
	pullCalls int
	failFrom  int
}

func (s *pullFailingServer) Pull(ctx context.Context, vault string, since int64) ([]Entry, int64, error) {
	s.mu.Lock()
	s.pullCalls++
	calls := s.pullCalls
	s.mu.Unlock()
	if calls >= s.failFrom {
		return nil, 0, errors.New("heal pull unavailable")
	}
	return s.serverAPI.Pull(ctx, vault, since)
}

func TestPushHealPullFailureKeepsStateAndReturnsNetworkError(t *testing.T) {
	srv := newFakeServer()
	wrapped := &pullFailingServer{serverAPI: srv, failFrom: 2}
	p, cache := newTestProvider(t, srv)
	p.api = wrapped
	writeEnvVar(t, cache, "default", "E", "value")
	if _, err := p.AutoPush(context.Background(), autoSyncBudget); err != nil {
		t.Fatal(err)
	}
	corruptSnapshotFor(t, cache, KindEnv, "default", "E")
	_, err := p.SyncWithReport(context.Background())
	var conflict *SyncConflictError
	if errors.As(err, &conflict) {
		t.Fatalf("heal pull failure must not be reported as content conflict: %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "heal pull unavailable") {
		t.Fatalf("err = %v, want propagated network error", err)
	}
	if _, ok := readStateFile(t, cache).Entries[entryID(KindEnv, "default", "E")]; ok {
		t.Error("failed heal must not persist adoption")
	}
}

func TestPushMetadataRealConflictStillErrors(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	localMeta := []byte(`{"salt":"local","password_key":"l"}`)
	if err := cache.writeMetadata(localMeta); err != nil {
		t.Fatal(err)
	}
	srv.metadata["main"] = []byte(`{"salt":"remote","password_key":"r"}`)
	_, err := p.SyncWithReport(context.Background())
	var conflict *SyncConflictError
	if !errors.As(err, &conflict) || !conflict.MetadataConflict {
		t.Fatalf("err = %v, want metadata SyncConflictError", err)
	}
}

// --- 3.2 故障报告场景端到端回归 ---

func TestSyncHealsConfigIndexAndMetadataEndToEnd(t *testing.T) {
	p, cache := newTestProvider(t, newFakeServer())
	writeEnvVar(t, cache, "default", "EXISTING", "keep")
	indexPath := filepath.Join(cache.configPath, "config_index.json")
	indexBytes := []byte(`{"version":"1.0","configs":{"db":{"name":"db","encrypted_file":"db.pem.enc","group":"","description":""}}}`)
	if err := os.WriteFile(indexPath, indexBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AutoPush(context.Background(), autoSyncBudget); err != nil {
		t.Fatal(err)
	}
	envBefore, _ := os.ReadFile(mustEntryPath(t, cache, KindEnv, "default", "EXISTING"))
	indexBefore, _ := os.ReadFile(indexPath)

	// 模拟事故形态：丢失 config_index 快照 + 清空 metadata_hash，revision 冻结。
	rewriteStateFile(t, cache, func(st *syncState) {
		delete(st.Entries, entryID(KindConfigIndex, "", ""))
		st.MetadataHash = ""
	})

	res, err := p.SyncWithReport(context.Background())
	if err != nil {
		t.Fatalf("事故形态应自动自愈而非永久冲突: %v", err)
	}
	if res.Healed != 2 {
		t.Errorf("Healed = %d, want 2 (config_index + metadata)", res.Healed)
	}
	envAfter, _ := os.ReadFile(mustEntryPath(t, cache, KindEnv, "default", "EXISTING"))
	indexAfter, _ := os.ReadFile(indexPath)
	if string(envBefore) != string(envAfter) || string(indexBefore) != string(indexAfter) {
		t.Fatal("自愈不得改动两端数据字节")
	}
	st := readStateFile(t, cache)
	snap, ok := st.Entries[entryID(KindConfigIndex, "", "")]
	if !ok || snap.Hash != hashBytes(indexBytes) {
		t.Fatalf("config_index 快照未收养: %+v", snap)
	}
	if st.MetadataHash == "" {
		t.Fatal("metadata 哈希未收养")
	}

	// 自愈后再次同步应真正"已是最新"且无 dirty。
	again, err := p.SyncWithReport(context.Background())
	if err != nil {
		t.Fatalf("自愈后二次同步失败: %v", err)
	}
	if again.Dirty != 0 || again.Healed != 0 {
		t.Errorf("二次同步 dirty=%d healed=%d, want 0/0", again.Dirty, again.Healed)
	}
}
