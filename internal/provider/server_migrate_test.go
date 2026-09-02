package provider

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/wii/senv/internal/storage"
	"time"
)

// TestE2EMigrateRoundtrip 双向迁移：git → server → git，口令不变、数据一致
func TestE2EMigrateRoundtrip(t *testing.T) {
	baseURL, token, _ := e2eEnv(t)
	ctx := context.Background()
	password := "migrate-password"

	// 源：git 模式本地 vault（env + text）
	cfgA, dataA, keyA := newLocalVault(t, password)
	smA := storage.NewManager(cfgA, dataA)
	entry2 := &storage.EnvVarEntry{Value: "v2", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := smA.SaveEnvVarWithKey("prod", "DB_URL", entry2, keyA); err != nil {
		t.Fatalf("save second env: %v", err)
	}

	// to-server
	spA := NewServerProvider(baseURL, token, cfgA, dataA, "main")
	res, err := spA.MigrateToServer(ctx, false)
	if err != nil {
		t.Fatalf("MigrateToServer: %v", err)
	}
	if res.Counts[KindEnv] != 2 || !res.MetadataMoved {
		t.Errorf("result = %+v, want 2 env entries + metadata moved", res)
	}

	// from-server 到全新目录：用原口令解锁并读到一致数据
	cfgB, dataB := t.TempDir(), t.TempDir()
	spB := NewServerProvider(baseURL, token, cfgB, dataB, "main")
	res, err = spB.MigrateFromServer(ctx, false)
	if err != nil {
		t.Fatalf("MigrateFromServer: %v", err)
	}
	if res.Counts[KindEnv] != 2 || !res.MetadataMoved {
		t.Errorf("from-server result = %+v, want 2 env entries + metadata moved", res)
	}
	smB := storage.NewManager(cfgB, dataB)
	keyB := deriveKey(t, smB, password) // 口令不变验证
	got, err := smB.LoadEnvVarWithKey("prod", "DB_URL", keyB)
	if err != nil {
		t.Fatalf("load migrated env: %v", err)
	}
	if got.Value != "v2" {
		t.Errorf("migrated value = %q, want v2", got.Value)
	}
}

// TestE2EMigrateRetry 中断重试：已完成条目幂等跳过，迁移继续直至完成
func TestE2EMigrateRetry(t *testing.T) {
	baseURL, token, _ := e2eEnv(t)
	ctx := context.Background()
	password := "migrate-retry"

	cfgA, dataA, keyA := newLocalVault(t, password)
	smA := storage.NewManager(cfgA, dataA)
	for _, k := range []string{"K1", "K2", "K3"} {
		e := &storage.EnvVarEntry{Value: "v-" + k, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := smA.SaveEnvVarWithKey("default", k, e, keyA); err != nil {
			t.Fatalf("save %s: %v", k, err)
		}
	}

	// 模拟中断后的残留：手工推送一部分条目（与本地一致）+ metadata
	api := newServerClient(baseURL, token)
	cache := &localCache{configPath: cfgA, dataPath: dataA}
	local, err := cache.collect()
	if err != nil {
		t.Fatal(err)
	}
	var partial []Entry
	for _, e := range local {
		if e.Key == "K1" {
			partial = append(partial, e)
		}
	}
	if _, _, err := api.Push(ctx, "main", partial); err != nil {
		t.Fatalf("partial push: %v", err)
	}
	blob, _ := os.ReadFile(cache.metadataPath())
	if err := api.PutMetadata(ctx, "main", blob); err != nil {
		t.Fatalf("partial metadata: %v", err)
	}

	// 重跑完整迁移：一致条目跳过，其余继续，不再报"目标非空"
	spA := NewServerProvider(baseURL, token, cfgA, dataA, "main")
	res, err := spA.MigrateToServer(ctx, false)
	if err != nil {
		t.Fatalf("retry MigrateToServer: %v", err)
	}
	if res.Skipped != 1 || !res.MetadataSkipped {
		t.Errorf("retry result = skipped %d metadataSkipped %v, want 1/true", res.Skipped, res.MetadataSkipped)
	}
	if res.Counts[KindEnv] != 3 { // K2、K3 + API_KEY（newLocalVault 预置）
		t.Errorf("retry moved %d env entries, want 3", res.Counts[KindEnv])
	}

	// 再次执行：全部幂等跳过
	res, err = spA.MigrateToServer(ctx, false)
	if err != nil {
		t.Fatalf("third MigrateToServer: %v", err)
	}
	if res.Counts[KindEnv] != 0 {
		t.Errorf("fully converged migrate should move 0 entries, got %d", res.Counts[KindEnv])
	}
}

// TestE2EMigrateTargetNotEmpty 非空目标保护：未确认覆盖时中止且目标不变
func TestE2EMigrateTargetNotEmpty(t *testing.T) {
	baseURL, token, _ := e2eEnv(t)
	ctx := context.Background()
	password := "migrate-protect"

	// server 上已有"别人"的数据
	api := newServerClient(baseURL, token)
	if _, _, err := api.Push(ctx, "main", []Entry{
		{Kind: KindEnv, Grp: "default", Key: "FOREIGN", Ciphertext: []byte("other"), BaseRevision: 0},
	}); err != nil {
		t.Fatalf("seed foreign entry: %v", err)
	}
	if err := api.PutMetadata(ctx, "main", []byte("foreign-meta")); err != nil {
		t.Fatalf("seed foreign metadata: %v", err)
	}

	cfgA, dataA, _ := newLocalVault(t, password)
	spA := NewServerProvider(baseURL, token, cfgA, dataA, "main")

	// 未 --force：中止，目标数据不变
	_, err := spA.MigrateToServer(ctx, false)
	if err == nil {
		t.Fatal("migrate into non-empty vault should abort")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should hint --force, got: %v", err)
	}
	meta, _ := api.GetMetadata(ctx, "main")
	if string(meta) != "foreign-meta" {
		t.Errorf("remote metadata changed to %q after aborted migrate", meta)
	}
	entries, _, _ := api.Pull(ctx, "main", 0)
	for _, e := range entries {
		if e.Key == "API_KEY" {
			t.Error("local entry written despite aborted migrate")
		}
	}

	// --force：显式覆盖，以本地为准；远端额外条目保留并计数
	res, err := spA.MigrateToServer(ctx, true)
	if err != nil {
		t.Fatalf("force migrate: %v", err)
	}
	if res.ExtraKept != 1 {
		t.Errorf("ExtraKept = %d, want 1 (FOREIGN preserved)", res.ExtraKept)
	}
	meta, _ = api.GetMetadata(ctx, "main")
	localMeta, _ := os.ReadFile(cache2(cfgA).metadataPath())
	if string(meta) != string(localMeta) {
		t.Error("remote metadata should match local after force migrate")
	}
}

// TestE2EMigrateFromServerNotEmpty from-server 的本地非空保护
func TestE2EMigrateFromServerNotEmpty(t *testing.T) {
	baseURL, token, _ := e2eEnv(t)
	ctx := context.Background()
	password := "migrate-from-protect"

	// server 上有数据
	cfgA, dataA, _ := newLocalVault(t, password)
	spA := NewServerProvider(baseURL, token, cfgA, dataA, "main")
	if _, err := spA.MigrateToServer(ctx, false); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	// 本地目标已有不同内容（不同口令初始化的 vault → metadata 不同）
	cfgB, dataB, _ := newLocalVault(t, "different-password")
	spB := NewServerProvider(baseURL, token, cfgB, dataB, "main")
	if _, err := spB.MigrateFromServer(ctx, false); err == nil {
		t.Fatal("from-server into non-empty local should abort")
	}
	// 本地数据未被改动
	smB := storage.NewManager(cfgB, dataB)
	if ok, _ := smB.VerifyPassword("different-password"); !ok {
		t.Error("local vault modified despite aborted from-server migrate")
	}

	// --force 后以远端为准，口令切换为源 vault 口令
	if _, err := spB.MigrateFromServer(ctx, true); err != nil {
		t.Fatalf("force from-server: %v", err)
	}
	if ok, _ := smB.VerifyPassword(password); !ok {
		t.Error("after force from-server, source password should unlock local vault")
	}
}

func cache2(configPath string) *localCache {
	return &localCache{configPath: configPath}
}
