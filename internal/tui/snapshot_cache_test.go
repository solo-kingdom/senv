package tui

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

// snapshotTestVault 建一个已初始化的临时 vault 及 key 形态的管理器。
func snapshotTestVault(t *testing.T) (Managers, *SnapshotCache, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "cfg")
	dataPath := filepath.Join(dir, "data")
	sm := storage.NewManager(configPath, dataPath)
	if err := sm.Initialize("test-password"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	md, err := sm.LoadMetadata()
	if err != nil {
		t.Fatalf("load metadata: %v", err)
	}
	salt, err := base64.StdEncoding.DecodeString(md.Salt)
	if err != nil {
		t.Fatalf("decode salt: %v", err)
	}
	iters, err := md.ValidatedKDFIterations()
	if err != nil {
		t.Fatalf("iterations: %v", err)
	}
	key := crypto.DeriveKeyWithIterations("test-password", salt, iters)
	mgrs := Managers{
		Env:    env.NewManagerWithKey(sm, key),
		Text:   text.NewManagerWithKey(sm, key),
		Config: nil,
	}
	cache := NewSnapshotCache(dataPath, configPath, key)
	mgrs.SnapshotCache = cache
	return mgrs, cache, dataPath
}

// seedSnapshotData 写入已知明文，供密文性质断言使用。
func seedSnapshotData(t *testing.T, mgrs Managers) {
	t.Helper()
	if err := mgrs.Env.Set("default", "API_KEY", "topsecret-value-xyz"); err != nil {
		t.Fatalf("seed env: %v", err)
	}
	if err := mgrs.Text.Set("notes", "readme-block", "confidential-body-123"); err != nil {
		t.Fatalf("seed text: %v", err)
	}
}

func TestSnapshotCacheRoundTrip(t *testing.T) {
	mgrs, cache, dataPath := snapshotTestVault(t)
	seedSnapshotData(t, mgrs)

	if !cache.Write(mgrs) {
		t.Fatal("Write must succeed on a healthy vault")
	}
	info, err := os.Stat(filepath.Join(dataPath, snapshotCacheFileName))
	if err != nil {
		t.Fatalf("snapshot file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("snapshot perm = %o, want 0600", perm)
	}
	di, err := os.Stat(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Fatalf("data dir perm = %o, want 0700", perm)
	}

	payload := cache.TryLoad()
	if payload == nil {
		t.Fatal("TryLoad must hit after Write")
	}
	if payload.Env.Vars["default"]["API_KEY"] != "topsecret-value-xyz" {
		t.Fatalf("env round-trip = %q", payload.Env.Vars["default"]["API_KEY"])
	}
	items := payload.Text.Items["notes"]
	if len(items) != 1 || items[0].Key != "readme-block" {
		t.Fatalf("text round-trip = %+v", items)
	}
	if payload.Fingerprint == "" {
		t.Fatal("fingerprint must be recorded")
	}
}

// TestSnapshotCacheCiphertextNoPlaintext 断言快照文件内不出现任何已知明文
// 子串（env 值、text 内容、text key），即静态内容确实是密文。
func TestSnapshotCacheCiphertextNoPlaintext(t *testing.T) {
	mgrs, cache, dataPath := snapshotTestVault(t)
	seedSnapshotData(t, mgrs)
	if !cache.Write(mgrs) {
		t.Fatal("Write failed")
	}
	blob, err := os.ReadFile(filepath.Join(dataPath, snapshotCacheFileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, plaintext := range []string{
		"topsecret-value-xyz", "confidential-body-123", "readme-block", "API_KEY",
	} {
		if strings.Contains(string(blob), plaintext) {
			t.Fatalf("snapshot file contains plaintext %q", plaintext)
		}
	}
}

func TestSnapshotCacheTamperFallback(t *testing.T) {
	mgrs, cache, dataPath := snapshotTestVault(t)
	seedSnapshotData(t, mgrs)
	if !cache.Write(mgrs) {
		t.Fatal("Write failed")
	}
	path := filepath.Join(dataPath, snapshotCacheFileName)
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	blob[len(blob)/2] ^= 0xff
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	if payload := cache.TryLoad(); payload != nil {
		t.Fatal("tampered snapshot must silently miss")
	}
}

func TestSnapshotCacheFingerprintMismatch(t *testing.T) {
	mgrs, cache, _ := snapshotTestVault(t)
	seedSnapshotData(t, mgrs)
	if !cache.Write(mgrs) {
		t.Fatal("Write failed")
	}
	// vault 变更后不重写：指纹失配 → 未命中（回退直接解密路径）
	if err := mgrs.Env.Set("default", "NEW_KEY", "v"); err != nil {
		t.Fatal(err)
	}
	if payload := cache.TryLoad(); payload != nil {
		t.Fatal("stale snapshot (fingerprint mismatch) must miss")
	}
	// 重写后恢复命中，且包含新条目
	if !cache.Write(mgrs) {
		t.Fatal("rewrite failed")
	}
	payload := cache.TryLoad()
	if payload == nil {
		t.Fatal("snapshot must hit after rewrite")
	}
	if payload.Env.Vars["default"]["NEW_KEY"] != "v" {
		t.Fatal("rewritten snapshot must contain the new entry")
	}
}

func TestSnapshotCacheMissingFileSilentMiss(t *testing.T) {
	_, cache, _ := snapshotTestVault(t)
	if payload := cache.TryLoad(); payload != nil {
		t.Fatal("missing snapshot file must silently miss")
	}
}

func TestSnapshotCacheDisabledOrNoKey(t *testing.T) {
	mgrs, cache, _ := snapshotTestVault(t)
	t.Setenv(EnvSnapshotOff, "off")
	if NewSnapshotCache(cache.dataPath, cache.configPath, cache.key) != nil {
		t.Fatal("SENV_TUI_SNAPSHOT=off must disable the cache")
	}
	if NewSnapshotCache(cache.dataPath, cache.configPath, nil) != nil {
		t.Fatal("nil key must disable the cache")
	}
	_ = mgrs
}

// TestRegistrySeedAndVerifyCache 验证预热 + 后台逐域比对：缓存命中时 memo
// 直接服务读取；真实数据不一致时替换并报告 changed。
func TestRegistrySeedAndVerifyCache(t *testing.T) {
	mgrs, cache, _ := snapshotTestVault(t)
	seedSnapshotData(t, mgrs)
	if !cache.Write(mgrs) {
		t.Fatal("Write failed")
	}
	payload := cache.TryLoad()
	if payload == nil {
		t.Fatal("precondition: cache hit")
	}

	m := New(mgrs)
	if !m.cacheSeeded {
		t.Fatal("New must seed the registry from a valid cache")
	}
	snap, err := m.mgr.snap.Get()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Vars["default"]["API_KEY"] != "topsecret-value-xyz" {
		t.Fatal("seeded memo must serve cached env data")
	}

	// 未变更：校验报告无变化，memo 保持
	if m.mgr.snap.VerifyCache() {
		t.Fatal("VerifyCache must report no change on an untouched vault")
	}
	if got := m.mgr.snap.envSnap.Vars["default"]["API_KEY"]; got != "topsecret-value-xyz" {
		t.Fatalf("memo drifted without change: %q", got)
	}

	// 真实数据变更（绕过 Invalidate，模拟缓存窗口内的外部写入）：校验
	// 必须发现并替换为真实数据
	if err := mgrs.Env.Set("default", "API_KEY", "rotated-value"); err != nil {
		t.Fatal(err)
	}
	if !m.mgr.snap.VerifyCache() {
		t.Fatal("VerifyCache must report change after a real write")
	}
	if got := m.mgr.snap.envSnap.Vars["default"]["API_KEY"]; got != "rotated-value" {
		t.Fatalf("memo must be replaced with real data, got %q", got)
	}
}

// TestSnapshotVerifyMsgReloadsActivatedTab 走完整消息循环：缓存预热渲染
// 后，后台校验发现不一致 → model 重载已激活 tab，展示替换为真实数据。
func TestSnapshotVerifyMsgReloadsActivatedTab(t *testing.T) {
	mgrs, cache, _ := snapshotTestVault(t)
	seedSnapshotData(t, mgrs)
	if !cache.Write(mgrs) {
		t.Fatal("Write failed")
	}
	m := New(mgrs)
	if !m.cacheSeeded {
		t.Fatal("precondition: seeded")
	}
	// 缓存窗口内真实数据被改写（绕过 Invalidate，模拟外部写入）
	if err := mgrs.Env.Set("default", "API_KEY", "rotated-value"); err != nil {
		t.Fatal(err)
	}
	out, cmd := m.Update(snapshotVerifiedMsg{changed: true})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("changed verify must trigger a reload command")
	}
	for _, msg := range runCmd(cmd) {
		out, _ = m.Update(msg)
		m = out.(Model)
	}
	envTab := m.tabs[m.active].(*envTab)
	found := false
	for _, it := range envTab.itemsByGroup["default"] {
		if it.key == "API_KEY" {
			found = true
		}
	}
	if !found {
		t.Fatal("activated env tab must reload to the real data after verify mismatch")
	}
}
