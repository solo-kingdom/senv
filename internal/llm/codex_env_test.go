package llm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/ref"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

// codexEnvKeyIn 读取 codex 配置里 senv 写入的 provider 的 env_key（ADR-0024 契约）。
func codexEnvKeyIn(t *testing.T, configPath, alias string) string {
	t.Helper()
	var cfg map[string]any
	if err := toml.Unmarshal(mustRead(t, configPath), &cfg); err != nil {
		t.Fatalf("parse codex TOML: %v", err)
	}
	providers, ok := cfg["model_providers"].(map[string]any)
	if !ok {
		t.Fatalf("config has no model_providers: %v", cfg)
	}
	provider, ok := providers[senvProviderID(alias)].(map[string]any)
	if !ok {
		t.Fatalf("config has no provider %s: %v", senvProviderID(alias), providers)
	}
	key, _ := provider["env_key"].(string)
	return key
}

// TestCodexEnvPlanFor 覆盖名字决议：env: 引用复用 key 名，text: 引用派生名 +
// 默认组兜底引用，非法引用报错。
func TestCodexEnvPlanFor(t *testing.T) {
	plan, err := codexEnvPlanFor(&storage.LLMProviderEntry{CredentialRef: "env:ai/K"}, "prov")
	if err != nil || plan.Name != "K" || plan.SeedRef != "" {
		t.Fatalf("env plan = %+v, err = %v", plan, err)
	}
	plan, err = codexEnvPlanFor(&storage.LLMProviderEntry{CredentialRef: "text:llm-keys/prov"}, "my-prov")
	if err != nil || plan.Name != "SENV_MY_PROV_API_KEY" || plan.SeedRef != "{{text:llm-keys:prov}}" {
		t.Fatalf("text plan = %+v, err = %v", plan, err)
	}
	for _, bad := range []string{"bogus", "env:noKey", "vault:ai/K"} {
		if _, err := codexEnvPlanFor(&storage.LLMProviderEntry{CredentialRef: bad}, "prov"); err == nil {
			t.Fatalf("ref %q unexpectedly accepted", bad)
		}
	}
}

// TestCodexEnvKeyReusesReferencedEnvName：env: 引用时 env_key 就是导出集合里
// 已有的名字，切换不写任何 env 条目、不产生 warning。
func TestCodexEnvKeyReusesReferencedEnvName(t *testing.T) {
	pm, store, _ := newTestProviderManager(t)
	envMgr := env.NewManager(store, "test-password")
	if err := envMgr.AddGroup("ai", "test"); err != nil {
		t.Fatal(err)
	}
	if err := envMgr.Set("ai", "DEEPSEEK_API_KEY", "sk-deepseek"); err != nil {
		t.Fatalf("env.Set() error = %v", err)
	}
	if err := envMgr.ActivateGroup("ai"); err != nil {
		t.Fatalf("ActivateGroup() error = %v", err)
	}
	addTestProvider(t, pm, AddProviderOptions{
		Alias: "deepseek", BaseURL: "https://api.deepseek.com/v1",
		KeyRef: "env:ai/DEEPSEEK_API_KEY", Models: []string{"m1"},
	})

	home := t.TempDir()
	out, err := NewSwitchManager(pm, "", home).Switch("codex", "deepseek", nil, "")
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if out.CredentialEnv != "DEEPSEEK_API_KEY" {
		t.Fatalf("CredentialEnv = %q, want the referenced env key name", out.CredentialEnv)
	}
	if got := codexEnvKeyIn(t, out.ConfigPath, "deepseek"); got != "DEEPSEEK_API_KEY" {
		t.Fatalf("env_key = %q, want DEEPSEEK_API_KEY", got)
	}
	if len(out.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none", out.Warnings)
	}
	vars, _, err := envMgr.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if _, ok := vars[storage.ConfigDefaultGroup]["DEEPSEEK_API_KEY"]; ok {
		t.Fatal("switch added an env entry although the referenced name is already exported")
	}
	if vars["ai"]["DEEPSEEK_API_KEY"] != "sk-deepseek" {
		t.Fatalf("referenced entry changed: %v", vars["ai"])
	}
	exports, err := envMgr.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if !strings.Contains(exports, "export DEEPSEEK_API_KEY='sk-deepseek'") {
		t.Fatalf("env export lacks the env_key name codex reads:\n%s", exports)
	}
}

// TestCodexTextRefSeedsDefaultGroupReference：text: 引用没有 env 名可复用，
// 切换在默认组写入指向该条目的引用条目，导出时解析为同一凭据。
func TestCodexTextRefSeedsDefaultGroupReference(t *testing.T) {
	pm, store, _ := newTestProviderManager(t)
	addTestProvider(t, pm, AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-main", Models: []string{"m1"},
	})
	envMgr := env.NewManager(store, "test-password")

	home := t.TempDir()
	out, err := NewSwitchManager(pm, "", home).Switch("codex", "main", nil, "")
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if out.CredentialEnv != "SENV_MAIN_API_KEY" {
		t.Fatalf("CredentialEnv = %q", out.CredentialEnv)
	}
	if got := codexEnvKeyIn(t, out.ConfigPath, "main"); got != "SENV_MAIN_API_KEY" {
		t.Fatalf("env_key = %q", got)
	}
	seed, err := envMgr.Get(storage.ConfigDefaultGroup, "SENV_MAIN_API_KEY")
	if err != nil {
		t.Fatalf("seed entry missing: %v", err)
	}
	if seed != "{{text:llm-keys:main}}" {
		t.Fatalf("seed value = %q, want the text reference", seed)
	}
	if len(out.Warnings) != 1 || !strings.Contains(out.Warnings[0], "已在组 default 写入") {
		t.Fatalf("Warnings = %v, want the seed notice", out.Warnings)
	}
	// 引用必须能被 canonical resolver 解析为本次凭据（导出时同一路径）。
	resolved, err := ref.Resolve(seed, providerRefGetter{
		env:  envMgr,
		text: text.NewManager(store, "test-password"),
	}, ref.ResolveOptions{})
	if err != nil || resolved != "sk-main" {
		t.Fatalf("resolve(seed) = %q, err = %v", resolved, err)
	}
	exports, err := envMgr.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if !strings.Contains(exports, "export SENV_MAIN_API_KEY=") {
		t.Fatalf("env export lacks SENV_MAIN_API_KEY:\n%s", exports)
	}
}

// TestCodexEnvSeedKeepsExistingEntry：同名条目已存在时一律不覆盖，只提示。
func TestCodexEnvSeedKeepsExistingEntry(t *testing.T) {
	pm, store, _ := newTestProviderManager(t)
	addTestProvider(t, pm, AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-main", Models: []string{"m1"},
	})
	envMgr := env.NewManager(store, "test-password")
	if err := envMgr.Set(storage.ConfigDefaultGroup, "SENV_MAIN_API_KEY", "user-owned"); err != nil {
		t.Fatalf("env.Set() error = %v", err)
	}

	home := t.TempDir()
	out, err := NewSwitchManager(pm, "", home).Switch("codex", "main", nil, "")
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	got, err := envMgr.Get(storage.ConfigDefaultGroup, "SENV_MAIN_API_KEY")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != "user-owned" {
		t.Fatalf("existing entry overwritten: %q", got)
	}
	if len(out.Warnings) != 1 || !strings.Contains(out.Warnings[0], "已存在且不指向本次凭据") {
		t.Fatalf("Warnings = %v, want the occupied-name notice", out.Warnings)
	}
	if got := codexEnvKeyIn(t, out.ConfigPath, "main"); got != "SENV_MAIN_API_KEY" {
		t.Fatalf("env_key = %q", got)
	}
}

// TestCodexEnvKeyFromInactiveGroupWarns：名字只在未激活组里时导出拿不到，
// 提示激活而不是补写重复条目。
func TestCodexEnvKeyFromInactiveGroupWarns(t *testing.T) {
	pm, store, _ := newTestProviderManager(t)
	envMgr := env.NewManager(store, "test-password")
	if err := envMgr.AddGroup("dev", "test"); err != nil {
		t.Fatal(err)
	}
	if err := envMgr.Set("dev", "APP_KEY", "sk-dev"); err != nil {
		t.Fatalf("env.Set() error = %v", err)
	}
	addTestProvider(t, pm, AddProviderOptions{
		Alias: "stag", BaseURL: "https://api.example.com",
		KeyRef: "env:dev/APP_KEY", Models: []string{"m1"},
	})

	home := t.TempDir()
	out, err := NewSwitchManager(pm, "", home).Switch("codex", "stag", nil, "")
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if out.CredentialEnv != "APP_KEY" {
		t.Fatalf("CredentialEnv = %q", out.CredentialEnv)
	}
	if len(out.Warnings) != 1 {
		t.Fatalf("Warnings = %v, want one activation notice", out.Warnings)
	}
	for _, want := range []string{"APP_KEY", "dev", "senv env group activate dev"} {
		if !strings.Contains(out.Warnings[0], want) {
			t.Errorf("warning %q missing %q", out.Warnings[0], want)
		}
	}
	vars, _, err := envMgr.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if _, ok := vars[storage.ConfigDefaultGroup]["APP_KEY"]; ok {
		t.Fatal("switch must not duplicate a name that already exists in an inactive group")
	}
}

// TestCodexMissingCredentialWritesNothing：codex 现在也解密凭据，引用缺失时
// fail-closed（ADR-0019），agent 配置与指针零写入。
func TestCodexMissingCredentialWritesNothing(t *testing.T) {
	pm, store, _ := newTestProviderManager(t)
	now := time.Now()
	if err := store.SaveLLMProvider("ghost", &storage.LLMProviderEntry{
		Alias: "ghost", BaseURL: "https://api.example.com",
		CredentialRef: "env:ghost/KEY", Models: []string{"m1"},
		DefaultModel: "m1", CreatedAt: now, UpdatedAt: now,
	}, "test-password"); err != nil {
		t.Fatalf("SaveLLMProvider() error = %v", err)
	}

	home := t.TempDir()
	_, err := NewSwitchManager(pm, "", home).Switch("codex", "ghost", nil, "")
	if err == nil {
		t.Fatal("Switch() unexpectedly succeeded without the credential")
	}
	for _, want := range []string{"env:ghost/KEY", "senv env set ghost KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Errorf("codex config written despite missing credential: %v", err)
	}
	if _, err := os.Stat(codexCatalogPath(home, "ghost")); !os.IsNotExist(err) {
		t.Errorf("codex catalog written despite missing credential: %v", err)
	}
	if _, err := os.Stat(DefaultPointerPath(home)); !os.IsNotExist(err) {
		t.Errorf("pointer written despite missing credential: %v", err)
	}
}

// TestCodexEnvSeedFailureWritesNothing：兜底写入失败时切换失败且零配置写入
// （决议先于写回，见 ADR-0024）。
func TestCodexEnvSeedFailureWritesNothing(t *testing.T) {
	base := t.TempDir()
	store := storage.NewManager(filepath.Join(base, "config"), filepath.Join(base, "data"))
	if err := store.Initialize("test-password"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	pm := NewProviderManager(store, "test-password")
	src := env.NewManager(store, "test-password")
	if err := src.Set(storage.ConfigDefaultGroup, "OTHER", "keep"); err != nil {
		t.Fatalf("env.Set() error = %v", err)
	}
	if _, err := pm.AddProvider(AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-main", Models: []string{"m1"},
	}); err != nil {
		t.Fatalf("AddProvider() error = %v", err)
	}

	// 只读默认组目录，使兜底写入失败；chmod 对特权进程无效时跳过（同
	// internal/tui 的既有做法）。
	groupDir := filepath.Join(base, "data", storage.EnvDirName, storage.ConfigDefaultGroup)
	if err := os.Chmod(groupDir, 0o555); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(groupDir, 0o700) })
	probe := filepath.Join(groupDir, "probe.enc")
	if err := os.WriteFile(probe, []byte("x"), 0o600); err == nil {
		_ = os.Remove(probe)
		t.Skip("chmod does not block writes in this environment")
	}

	home := t.TempDir()
	_, err := NewSwitchManager(pm, "", home).Switch("codex", "main", nil, "")
	if err == nil {
		t.Fatal("Switch() unexpectedly succeeded with an unwritable env group")
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Errorf("codex config written despite seeding failure: %v", err)
	}
	if _, err := os.Stat(codexCatalogPath(home, "main")); !os.IsNotExist(err) {
		t.Errorf("codex catalog written despite seeding failure: %v", err)
	}
	if _, err := os.Stat(DefaultPointerPath(home)); !os.IsNotExist(err) {
		t.Errorf("pointer written despite seeding failure: %v", err)
	}
}

// anyWarningContains 判断任意一条 warning 含指定子串（warning 顺序不是契约）。
func anyWarningContains(warnings []string, want string) bool {
	for _, w := range warnings {
		if strings.Contains(w, want) {
			return true
		}
	}
	return false
}
