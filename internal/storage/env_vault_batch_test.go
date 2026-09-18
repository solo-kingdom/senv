package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wii/senv/internal/securefs"
)

// TestLoadEnvVaultMatchesPerGroupLoad 验证批量装载路径（单锁单 root）与
// 逐分组路径的读取结果逐字段等价（tui-perf-load 的正确性前提）。
func TestLoadEnvVaultMatchesPerGroupLoad(t *testing.T) {
	mgr, configPath := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")

	want := map[string]map[string]string{
		"default": {"A": "1", "B": "2"},
		"prod":    {"API_KEY": "secret123"},
		"zeta":    {},
	}
	for name, vars := range want {
		grp := NewEnvGroup(name)
		for k, v := range vars {
			grp.Variables[k] = v
		}
		if err := mgr.SaveEnvGroupWithKey(grp, key); err != nil {
			t.Fatalf("save group %s: %v", name, err)
		}
	}

	batch, err := mgr.LoadEnvVaultWithKey(key)
	if err != nil {
		t.Fatalf("LoadEnvVaultWithKey: %v", err)
	}
	if len(batch) != len(want) {
		t.Fatalf("batch groups = %d, want %d", len(batch), len(want))
	}
	for name, wantVars := range want {
		got, ok := batch[name]
		if !ok {
			t.Fatalf("batch missing group %q", name)
		}
		if got.Name != name {
			t.Errorf("group %q name = %q", name, got.Name)
		}
		if len(got.Variables) != len(wantVars) {
			t.Fatalf("group %q vars = %d, want %d", name, len(got.Variables), len(wantVars))
		}
		for k, v := range wantVars {
			if got.Variables[k] != v {
				t.Errorf("group %q var %q = %q, want %q", name, k, got.Variables[k], v)
			}
		}
		// 与逐分组路径对照
		perGroup, err := mgr.LoadEnvGroupWithKey(name, key)
		if err != nil {
			t.Fatalf("LoadEnvGroupWithKey(%q): %v", name, err)
		}
		if perGroup.Name != got.Name || len(perGroup.Variables) != len(got.Variables) {
			t.Errorf("group %q: batch and per-group paths disagree", name)
		}
		for k, v := range got.Variables {
			if perGroup.Variables[k] != v {
				t.Errorf("group %q var %q: batch=%q per-group=%q", name, k, v, perGroup.Variables[k])
			}
		}
	}

	// 批量结果对新写可见（写后读不经过陈旧缓存）
	grp := NewEnvGroup("prod")
	grp.Variables["API_KEY"] = "rotated"
	if err := mgr.SaveEnvGroupWithKey(grp, key); err != nil {
		t.Fatalf("rewrite prod: %v", err)
	}
	batch2, err := mgr.LoadEnvVaultWithKey(key)
	if err != nil {
		t.Fatalf("reload after write: %v", err)
	}
	if batch2["prod"].Variables["API_KEY"] != "rotated" {
		t.Fatalf("batch read after write = %q, want rotated", batch2["prod"].Variables["API_KEY"])
	}
	_ = configPath
}

func TestSaveEnvGroupWithKeyPreservesEntryTimestamps(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")

	created := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	entry := &EnvVarEntry{Value: "v1", CreatedAt: created, UpdatedAt: created}
	if err := mgr.SaveEnvVarWithKey("hist", "TOKEN", entry, key); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveEnvGroupMetaWithKey("hist", &EnvGroupMeta{Name: "hist", CreatedAt: created}, key); err != nil {
		t.Fatal(err)
	}

	grp := NewEnvGroup("hist")
	grp.Variables["TOKEN"] = "v1"
	if err := mgr.SaveEnvGroupWithKey(grp, key); err != nil {
		t.Fatal(err)
	}
	got, err := mgr.LoadEnvVarWithKey("hist", "TOKEN", key)
	if err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, created)
	}
	if !got.UpdatedAt.Equal(created) {
		t.Fatalf("UpdatedAt = %v, want preserved", got.UpdatedAt)
	}

	grp.Variables["TOKEN"] = "v2"
	if err := mgr.SaveEnvGroupWithKey(grp, key); err != nil {
		t.Fatal(err)
	}
	got, err = mgr.LoadEnvVarWithKey("hist", "TOKEN", key)
	if err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt after value change = %v, want %v", got.CreatedAt, created)
	}
	if !got.UpdatedAt.After(created) {
		t.Fatalf("UpdatedAt should advance on value change, got %v", got.UpdatedAt)
	}
}

// TestManifestCacheSeesUnfinishedJournal 验证 manifest 缓存的失效界：缓存
// 预热（无 manifest）后出现伪造的未完成 rekey journal，必须走完整清算路径
// 并要求恢复，而不是命中缓存跳过。
func TestManifestCacheSeesUnfinishedJournal(t *testing.T) {
	mgr, _ := setupTestManager(t)
	_ = derivedKey(t, mgr, "test-password")

	// 预热缓存：一次空变更（vault 无 manifest）
	if err := mgr.WithVaultMutation(func(*Manager) error { return nil }); err != nil {
		t.Fatalf("warm mutation: %v", err)
	}

	// 伪造未完成 rekey journal：必须经 securefs root 写入（root 对非受管
	// 写入不可见，与真实 rekey 事务的写入方式一致），内容非法 → 恢复路径报错
	jroot, err := securefs.OpenRoot(mgr.configPath)
	if err != nil {
		t.Fatalf("open config root: %v", err)
	}
	if err := jroot.AtomicWrite([]string{rekeyManifestFile}, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write journal: %v", err)
	}
	jroot.Close()
	err = mgr.WithVaultMutation(func(*Manager) error { return nil })
	if !errors.Is(err, ErrRekeyRecoveryRequired) {
		t.Fatalf("unfinished journal must require recovery, got %v", err)
	}

	// journal 消失后恢复常规路径（缓存按存在性失效重算）
	if err := os.Remove(filepath.Join(mgr.configPath, rekeyManifestFile)); err != nil {
		t.Fatalf("remove journal: %v", err)
	}
	if err := mgr.WithVaultMutation(func(*Manager) error { return nil }); err != nil {
		t.Fatalf("mutation after journal removal: %v", err)
	}
}
