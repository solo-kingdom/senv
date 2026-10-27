package llm

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/wii/senv/internal/env"
	"gopkg.in/yaml.v3"
)

// TestCredentialEnvModeOpencode 覆盖 opencode 的 "{env:VAR}" 引用写回：
// auto 模式走 CredentialEnvVar，配置里 apiKey 是引用串而非明文。
func TestCredentialEnvModeOpencode(t *testing.T) {
	pm, store, _ := newTestProviderManager(t)
	addTestProvider(t, pm, AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-secret", Models: []string{"m1"},
	})

	home := t.TempDir()
	out, err := NewSwitchManager(pm, "", home).Switch("opencode", "main", nil, "", "", CredentialModeAuto)
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if out.CredentialEnv == "" {
		t.Fatal("CredentialEnv = \"\", want an env var name under auto mode")
	}
	cfg := string(mustRead(t, out.ConfigPath))
	wantRef := "{env:" + out.CredentialEnv + "}"
	if !strings.Contains(cfg, wantRef) {
		t.Fatalf("opencode config missing %q:\n%s", wantRef, cfg)
	}
	if strings.Contains(cfg, "sk-secret") {
		t.Fatalf("opencode config contains plaintext credential:\n%s", cfg)
	}
	// 兜底条目必须能被 senv env export 解析为本次凭据。
	exports, err := env.NewManager(store, "test-password").Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if !strings.Contains(exports, out.CredentialEnv) {
		t.Fatalf("env export lacks %s:\n%s", out.CredentialEnv, exports)
	}
}

// TestCredentialEnvModePi 覆盖 pi 的 "$NAME" 插值写回。
func TestCredentialEnvModePi(t *testing.T) {
	pm, _, _ := newTestProviderManager(t)
	addTestProvider(t, pm, AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-secret", Models: []string{"m1"},
	})

	home := t.TempDir()
	out, err := NewSwitchManager(pm, "", home).Switch("pi", "main", nil, "", "", CredentialModeAuto)
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(mustRead(t, out.ConfigPath), &cfg); err != nil {
		t.Fatalf("parse pi models.json: %v", err)
	}
	providers := cfg["providers"].(map[string]any)
	entry := providers[senvProviderID("main")].(map[string]any)
	wantRef := "$" + out.CredentialEnv
	if entry["apiKey"] != wantRef {
		t.Fatalf("pi apiKey = %v, want %q", entry["apiKey"], wantRef)
	}
}

// TestCredentialEnvModeOmp 覆盖 omp 的裸环境变量名写回（先按 env 解析）。
func TestCredentialEnvModeOmp(t *testing.T) {
	pm, _, _ := newTestProviderManager(t)
	addTestProvider(t, pm, AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-secret", Models: []string{"m1"},
	})

	home := t.TempDir()
	out, err := NewSwitchManager(pm, "", home).Switch("omp", "main", nil, "", "", CredentialModeAuto)
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(mustRead(t, out.ConfigPath), &cfg); err != nil {
		t.Fatalf("parse omp models.yml: %v", err)
	}
	providers := cfg["providers"].(map[string]any)
	entry := providers[senvProviderID("main")].(map[string]any)
	if entry["apiKey"] != out.CredentialEnv {
		t.Fatalf("omp apiKey = %v, want the bare env name %q", entry["apiKey"], out.CredentialEnv)
	}
}

// TestCredentialEnvModeKimi 覆盖 kimi 的 api_key_env 专用字段写回（与 api_key
// 互斥）。
func TestCredentialEnvModeKimi(t *testing.T) {
	pm, _, _ := newTestProviderManager(t)
	addTestProvider(t, pm, AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-secret", Models: []string{"m1"},
	})

	home := t.TempDir()
	out, err := NewSwitchManager(pm, "", home).Switch("kimi", "main", nil, "", "", CredentialModeAuto)
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	var cfg map[string]any
	if err := toml.Unmarshal(mustRead(t, out.ConfigPath), &cfg); err != nil {
		t.Fatalf("parse kimi config.toml: %v", err)
	}
	providers := cfg["providers"].(map[string]any)
	entry := providers[senvProviderID("main")].(map[string]any)
	if entry["api_key_env"] != out.CredentialEnv {
		t.Fatalf("kimi api_key_env = %v, want %q", entry["api_key_env"], out.CredentialEnv)
	}
	if _, ok := entry["api_key"]; ok {
		t.Fatalf("kimi config must not write api_key when api_key_env is set: %v", entry)
	}
}

// TestCredentialEnvModeClaudeCode 覆盖 claude-code 的两档语义（ADR-0031 修订）：
// auto 回落 inline（settings.json 写明文，无 senv session 也能起）；显式 env
// 才用契约名 ANTHROPIC_AUTH_TOKEN / ANTHROPIC_BASE_URL——settings.json 不写这
// 两个键，改由默认 env 组带归属标记的条目经 `senv env export` 提供。
func TestCredentialEnvModeClaudeCode(t *testing.T) {
	pm, store, _ := newTestProviderManager(t)
	addTestProvider(t, pm, AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-secret", Models: []string{"m1"},
	})
	envMgr := env.NewManager(store, "test-password")

	home := t.TempDir()
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	// 显式 env：settings 不写 ANTHROPIC_*，默认组出现带标记的契约条目。
	out, err := NewSwitchManager(pm, "", home).Switch("claude-code", "main", nil, "", "", CredentialModeEnv)
	if err != nil {
		t.Fatalf("env Switch() error = %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(mustRead(t, settingsPath), &cfg); err != nil {
		t.Fatalf("parse settings.json: %v", err)
	}
	envBlock := cfg["env"].(map[string]any)
	if _, ok := envBlock["ANTHROPIC_AUTH_TOKEN"]; ok {
		t.Fatalf("ANTHROPIC_AUTH_TOKEN must be removed under env mode: %v", envBlock)
	}
	if _, ok := envBlock["ANTHROPIC_BASE_URL"]; ok {
		t.Fatalf("ANTHROPIC_BASE_URL must be removed under env mode: %v", envBlock)
	}
	if out.CredentialEnv != claudeCredentialEnvName {
		t.Fatalf("CredentialEnv = %q, want %q", out.CredentialEnv, claudeCredentialEnvName)
	}
	if out.BaseURLEnv != claudeBaseURLEnvName {
		t.Fatalf("BaseURLEnv = %q, want %q", out.BaseURLEnv, claudeBaseURLEnvName)
	}
	token, tokenDesc := envEntryMeta(t, envMgr, "default", claudeCredentialEnvName)
	if token != "{{text:llm-keys:main}}" {
		t.Fatalf("ANTHROPIC_AUTH_TOKEN = %q, want the text ref of the provider credential", token)
	}
	if tokenDesc != claudeCredentialEnvDesc {
		t.Fatalf("ANTHROPIC_AUTH_TOKEN description = %q, want the senv-managed marker", tokenDesc)
	}
	baseURL, baseURLDesc := envEntryMeta(t, envMgr, "default", claudeBaseURLEnvName)
	if baseURL != "https://api.example.com" {
		t.Fatalf("ANTHROPIC_BASE_URL = %q, want the resolved base URL", baseURL)
	}
	if baseURLDesc != claudeBaseURLEnvDesc {
		t.Fatalf("ANTHROPIC_BASE_URL description = %q, want the senv-managed marker", baseURLDesc)
	}
	exports, err := envMgr.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if !strings.Contains(exports, claudeCredentialEnvName) || !strings.Contains(exports, claudeBaseURLEnvName) {
		t.Fatalf("env export lacks %s / %s:\n%s", claudeCredentialEnvName, claudeBaseURLEnvName, exports)
	}

	// auto：claude-code 的 CredentialEnvRef 返回空串 → 回落 inline（写明文），
	// 同时清掉上一轮 senv 归属的 env 条目。
	autoOut, err := NewSwitchManager(pm, "", home).Switch("claude-code", "main", nil, "", "", CredentialModeAuto)
	if err != nil {
		t.Fatalf("auto Switch() error = %v", err)
	}
	if autoOut.CredentialEnv != "" || autoOut.BaseURLEnv != "" {
		t.Fatalf("auto mode env output = %q/%q, want inline", autoOut.CredentialEnv, autoOut.BaseURLEnv)
	}
	changed := map[string]any{}
	if err := json.Unmarshal(mustRead(t, settingsPath), &changed); err != nil {
		t.Fatalf("parse settings.json after auto: %v", err)
	}
	autoEnv := changed["env"].(map[string]any)
	if autoEnv["ANTHROPIC_AUTH_TOKEN"] != "sk-secret" {
		t.Fatalf("auto mode must write plaintext credential: %v", autoEnv)
	}
	if autoEnv["ANTHROPIC_BASE_URL"] != "https://api.example.com" {
		t.Fatalf("auto mode must write plaintext base URL: %v", autoEnv)
	}
	for _, name := range []string{claudeCredentialEnvName, claudeBaseURLEnvName} {
		if _, _, err := envMgr.GetWithMeta("default", name); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("env entry %s must be cleaned after switching to auto: err = %v", name, err)
		}
	}
}

// TestCredentialEnvModeClaudeCodeSwitchProvider 覆盖归属条目的更新语义：反复
// 切 provider 直接覆盖带标记条目的值，不产生重复条目。
func TestCredentialEnvModeClaudeCodeSwitchProvider(t *testing.T) {
	pm, store, _ := newTestProviderManager(t)
	addTestProvider(t, pm, AddProviderOptions{
		Alias: "main", BaseURL: "https://a.example.com",
		APIKey: "sk-main", Models: []string{"m1"},
	})
	addTestProvider(t, pm, AddProviderOptions{
		Alias: "alt", BaseURL: "https://b.example.com",
		APIKey: "sk-alt", Models: []string{"m2"},
	})
	envMgr := env.NewManager(store, "test-password")
	home := t.TempDir()

	if _, err := NewSwitchManager(pm, "", home).Switch("claude-code", "main", nil, "", "", CredentialModeEnv); err != nil {
		t.Fatalf("Switch(main) error = %v", err)
	}
	out, err := NewSwitchManager(pm, "", home).Switch("claude-code", "alt", nil, "", "", CredentialModeEnv)
	if err != nil {
		t.Fatalf("Switch(alt) error = %v", err)
	}
	token, tokenDesc := envEntryMeta(t, envMgr, "default", claudeCredentialEnvName)
	if token != "{{text:llm-keys:alt}}" {
		t.Fatalf("ANTHROPIC_AUTH_TOKEN = %q, want the new provider ref after switching", token)
	}
	if tokenDesc != claudeCredentialEnvDesc {
		t.Fatalf("marker lost after update: %q", tokenDesc)
	}
	baseURL, baseURLDesc := envEntryMeta(t, envMgr, "default", claudeBaseURLEnvName)
	if baseURL != "https://b.example.com" || baseURLDesc != claudeBaseURLEnvDesc {
		t.Fatalf("ANTHROPIC_BASE_URL = %q / %q, want updated value + marker", baseURL, baseURLDesc)
	}
	if out.CredentialEnv != claudeCredentialEnvName {
		t.Fatalf("CredentialEnv = %q", out.CredentialEnv)
	}
}

// TestCredentialEnvModeClaudeCodeUserEntryConflict 覆盖 fail closed：同名条目
// 被用户占用（无标记、值不同）时切换报可行动错误且零写盘。
func TestCredentialEnvModeClaudeCodeUserEntryConflict(t *testing.T) {
	pm, store, _ := newTestProviderManager(t)
	addTestProvider(t, pm, AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-secret", Models: []string{"m1"},
	})
	envMgr := env.NewManager(store, "test-password")
	if err := envMgr.Set("default", claudeCredentialEnvName, "user-owned-token"); err != nil {
		t.Fatalf("seed user entry: %v", err)
	}
	home := t.TempDir()

	_, err := NewSwitchManager(pm, "", home).Switch("claude-code", "main", nil, "", "", CredentialModeEnv)
	if err == nil {
		t.Fatal("switch unexpectedly succeeded over a user-owned env entry")
	}
	for _, want := range []string{claudeCredentialEnvName, "占用", "credential-mode=inline"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to mention %q", err, want)
		}
	}
	if _, statErr := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(statErr) {
		t.Fatalf("conflict must not write any config file: %v", statErr)
	}
	value, desc := envEntryMeta(t, envMgr, "default", claudeCredentialEnvName)
	if value != "user-owned-token" || desc != "" {
		t.Fatalf("user entry mutated: %q / %q", value, desc)
	}
	if _, _, err := envMgr.GetWithMeta("default", claudeBaseURLEnvName); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("base URL entry must not be written after fail-closed: %v", err)
	}
}

// TestCredentialEnvModeClaudeCodeBaseURLConflict 覆盖两阶段写入：接入地址条目
// 冲突时凭据条目也不能落盘（不留下半写状态）。
func TestCredentialEnvModeClaudeCodeBaseURLConflict(t *testing.T) {
	pm, store, _ := newTestProviderManager(t)
	addTestProvider(t, pm, AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-secret", Models: []string{"m1"},
	})
	envMgr := env.NewManager(store, "test-password")
	if err := envMgr.Set("default", claudeBaseURLEnvName, "https://user.example.com"); err != nil {
		t.Fatalf("seed user entry: %v", err)
	}
	home := t.TempDir()

	_, err := NewSwitchManager(pm, "", home).Switch("claude-code", "main", nil, "", "", CredentialModeEnv)
	if err == nil || !strings.Contains(err.Error(), claudeBaseURLEnvName) {
		t.Fatalf("error = %v, want a base URL conflict", err)
	}
	if _, _, err := envMgr.GetWithMeta("default", claudeCredentialEnvName); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("credential entry must not be written when the base URL conflicts: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(statErr) {
		t.Fatalf("conflict must not write any config file: %v", statErr)
	}
}

// TestCredentialEnvModeClaudeCodeInactiveGroupEntry 覆盖只存在于未激活组时
// fail closed 并提示激活：导出不会包含它，静默成功等于留下不可用状态。
func TestCredentialEnvModeClaudeCodeInactiveGroupEntry(t *testing.T) {
	pm, store, _ := newTestProviderManager(t)
	addTestProvider(t, pm, AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-secret", Models: []string{"m1"},
	})
	envMgr := env.NewManager(store, "test-password")
	if err := envMgr.AddGroup("archive", "test"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
	if err := envMgr.Set("archive", claudeCredentialEnvName, "sk-secret"); err != nil {
		t.Fatalf("seed inactive entry: %v", err)
	}
	home := t.TempDir()

	_, err := NewSwitchManager(pm, "", home).Switch("claude-code", "main", nil, "", "", CredentialModeEnv)
	if err == nil {
		t.Fatal("switch unexpectedly succeeded with the entry only in an inactive group")
	}
	if !strings.Contains(err.Error(), "env group activate archive") {
		t.Fatalf("error = %v, want an activate hint", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(statErr) {
		t.Fatalf("inactive-group conflict must not write any config file: %v", statErr)
	}
}

// TestCredentialEnvModeClaudeCodeCleanupKeepsUserEntries 覆盖清理边界：env →
// inline 只删带标记的契约条目，用户自建条目与历史 SENV_<ALIAS>_* 一律保留。
func TestCredentialEnvModeClaudeCodeCleanupKeepsUserEntries(t *testing.T) {
	pm, store, _ := newTestProviderManager(t)
	addTestProvider(t, pm, AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-secret", Models: []string{"m1"},
	})
	envMgr := env.NewManager(store, "test-password")
	home := t.TempDir()
	sm := NewSwitchManager(pm, "", home)

	if _, err := sm.Switch("claude-code", "main", nil, "", "", CredentialModeEnv); err != nil {
		t.Fatalf("env Switch() error = %v", err)
	}
	// 用户自建同名条目（未激活组、无标记）与历史遗留派生条目。
	if err := envMgr.AddGroup("personal", "test"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
	if err := envMgr.Set("personal", claudeCredentialEnvName, "user-owned-token"); err != nil {
		t.Fatalf("seed user entry: %v", err)
	}
	if err := envMgr.Set("default", senvEnvKeyName("main"), "{{text:llm-keys:main}}"); err != nil {
		t.Fatalf("seed legacy entry: %v", err)
	}

	if _, err := sm.Switch("claude-code", "main", nil, "", "", CredentialModeInline); err != nil {
		t.Fatalf("inline Switch() error = %v", err)
	}
	for _, name := range []string{claudeCredentialEnvName, claudeBaseURLEnvName} {
		if _, _, err := envMgr.GetWithMeta("default", name); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("managed entry %s must be deleted on inline switch: err = %v", name, err)
		}
	}
	if value, _ := envEntryMeta(t, envMgr, "personal", claudeCredentialEnvName); value != "user-owned-token" {
		t.Fatalf("user-owned entry must survive cleanup, got %q", value)
	}
	if _, _, err := envMgr.GetWithMeta("default", senvEnvKeyName("main")); err != nil {
		t.Fatalf("legacy SENV_* entry must not be cleaned: %v", err)
	}
}

// TestCredentialEnvModeClaudeCodeEnvRef 覆盖 env:<g>/<k> 凭据引用：契约条目值
// 是 {{env:g:k}} 引用，而不是把被引用的 key 名当变量名。
func TestCredentialEnvModeClaudeCodeEnvRef(t *testing.T) {
	pm, store, _ := newTestProviderManager(t)
	envMgr := env.NewManager(store, "test-password")
	if err := envMgr.AddGroup("ai", "test"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
	if err := envMgr.Set("ai", "DEEPSEEK_API_KEY", "sk-deepseek"); err != nil {
		t.Fatalf("env.Set() error = %v", err)
	}
	if err := envMgr.ActivateGroup("ai"); err != nil {
		t.Fatalf("ActivateGroup() error = %v", err)
	}
	addTestProvider(t, pm, AddProviderOptions{
		Alias: "deepseek", BaseURL: "https://api.deepseek.com",
		KeyRef: "env:ai/DEEPSEEK_API_KEY", Models: []string{"m1"},
	})

	home := t.TempDir()
	out, err := NewSwitchManager(pm, "", home).Switch("claude-code", "deepseek", nil, "", "", CredentialModeEnv)
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if out.CredentialEnv != claudeCredentialEnvName {
		t.Fatalf("CredentialEnv = %q, want %q", out.CredentialEnv, claudeCredentialEnvName)
	}
	token, desc := envEntryMeta(t, envMgr, "default", claudeCredentialEnvName)
	if token != "{{env:ai:DEEPSEEK_API_KEY}}" {
		t.Fatalf("ANTHROPIC_AUTH_TOKEN = %q, want the {{env:...}} reference", token)
	}
	if desc != claudeCredentialEnvDesc {
		t.Fatalf("ANTHROPIC_AUTH_TOKEN description = %q", desc)
	}
	exports, err := envMgr.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if !strings.Contains(exports, claudeCredentialEnvName) {
		t.Fatalf("env export must provide %s:\n%s", claudeCredentialEnvName, exports)
	}
}

// envEntryMeta 读取 env 条目的值与描述，缺失即测试失败。
func envEntryMeta(t *testing.T, mgr *env.Manager, group, key string) (string, string) {
	t.Helper()
	value, desc, err := mgr.GetWithMeta(group, key)
	if err != nil {
		t.Fatalf("GetWithMeta(%s, %s) error = %v", group, key, err)
	}
	return value, desc
}

// TestCredentialEnvModeInlineForced：强制 inline 时即使 adapter 支持 env 也
// 必须写明文（保持 ADR-0002 原始行为）。
func TestCredentialEnvModeInlineForced(t *testing.T) {
	pm, _, _ := newTestProviderManager(t)
	addTestProvider(t, pm, AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-secret", Models: []string{"m1"},
	})

	home := t.TempDir()
	out, err := NewSwitchManager(pm, "", home).Switch("opencode", "main", nil, "", "", CredentialModeInline)
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if out.CredentialEnv != "" {
		t.Fatalf("CredentialEnv = %q, want empty under forced inline", out.CredentialEnv)
	}
	cfg := string(mustRead(t, out.ConfigPath))
	if !strings.Contains(cfg, "sk-secret") {
		t.Fatalf("forced inline must write plaintext:\n%s", cfg)
	}
}
