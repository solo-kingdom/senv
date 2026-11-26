package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeClaudeSettings 写入带 env 块的 claude settings.json。
func writeClaudeSettings(t *testing.T, path string, env map[string]any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"env": env})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func readClaudeEnv(t *testing.T, path string) map[string]any {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(mustRead(t, path), &root); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	env, _ := root["env"].(map[string]any)
	if env == nil {
		t.Fatalf("env missing from %s", path)
	}
	return env
}

func claudeTierReq(a AgentAdapter, home, configPath string, models []string, defaultModel string) SwitchRequest {
	return SwitchRequest{
		AgentID: a.ID, ProviderAlias: "main", BaseURL: "https://api.example.com",
		Models: models, DefaultModel: defaultModel, Credential: "sk-secret",
		ConfigPath: configPath, Home: home,
	}
}

// 档案缩集后旧值失效：OPUS/SONNET 都应改写为本次默认模型的发送名并提示。
func TestClaudeCodeHealsStaleTierModels(t *testing.T) {
	a := claudeCodeAdapter()
	home := t.TempDir()
	configPath := a.ConfigPath(home)
	writeClaudeSettings(t, configPath, map[string]any{
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "old",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "old",
	})
	var warnings []string
	req := claudeTierReq(a, home, configPath, []string{"new"}, "new")
	req.warnings = &warnings
	if err := a.Apply(req); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	env := readClaudeEnv(t, configPath)
	for _, key := range claudeTierModelKeys {
		if got := env[key]; got != "new" {
			t.Fatalf("%s = %v, want new", key, got)
		}
	}
	if len(warnings) != len(claudeTierModelKeys) {
		t.Fatalf("warnings = %v, want one per healed key", warnings)
	}
	joined := strings.Join(warnings, "\n")
	for _, key := range claudeTierModelKeys {
		if !strings.Contains(joined, key) || !strings.Contains(joined, `"old"`) {
			t.Fatalf("warnings = %v, want %s and old value", warnings, key)
		}
	}
}

// 用户把档位钉到仍在模型集内的模型：切换不得覆盖，也不应告警。
func TestClaudeCodeKeepsValidTierModels(t *testing.T) {
	a := claudeCodeAdapter()
	home := t.TempDir()
	configPath := a.ConfigPath(home)
	writeClaudeSettings(t, configPath, map[string]any{
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "other",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "other",
	})
	var warnings []string
	req := claudeTierReq(a, home, configPath, []string{"new", "other"}, "new")
	req.warnings = &warnings
	if err := a.Apply(req); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	env := readClaudeEnv(t, configPath)
	for _, key := range claudeTierModelKeys {
		if got := env[key]; got != "other" {
			t.Fatalf("%s = %v, want user-pinned other", key, got)
		}
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none for valid tier values", warnings)
	}
}

// [1m] 后缀是发送名而不是模型 ID：去后缀后仍在模型集内就必须保留。
func TestClaudeCodeKeepsValidSuffixedTierModel(t *testing.T) {
	a := claudeCodeAdapter()
	home := t.TempDir()
	configPath := a.ConfigPath(home)
	pinned := "other" + claude1MSuffix
	writeClaudeSettings(t, configPath, map[string]any{
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   pinned,
		"ANTHROPIC_DEFAULT_SONNET_MODEL": pinned,
	})
	var warnings []string
	req := claudeTierReq(a, home, configPath, []string{"new", "other"}, "new")
	req.warnings = &warnings
	if err := a.Apply(req); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	env := readClaudeEnv(t, configPath)
	for _, key := range claudeTierModelKeys {
		if got := env[key]; got != pinned {
			t.Fatalf("%s = %v, want %q", key, got, pinned)
		}
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none for valid suffixed tier values", warnings)
	}
}

// 未设置档位键时不得因为自愈逻辑新增它们。
func TestClaudeCodeTierModelsAbsentStayAbsent(t *testing.T) {
	a := claudeCodeAdapter()
	home := t.TempDir()
	configPath := a.ConfigPath(home)
	req := claudeTierReq(a, home, configPath, []string{"new"}, "new")
	if err := a.Apply(req); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	env := readClaudeEnv(t, configPath)
	for _, key := range claudeTierModelKeys {
		if _, ok := env[key]; ok {
			t.Fatalf("%s must stay absent for fresh config: %v", key, env)
		}
	}
}

// Switch 级：自愈提示必须进入 SwitchOutput.Warnings（CLI 打到 stderr），
// 且失效档位按默认模型的发送名写入（1M 默认模型带 [1m]）。
func TestClaudeCodeStaleTierWarningThroughSwitch(t *testing.T) {
	pm, _, _ := newTestProviderManager(t)
	if _, err := pm.AddProvider(AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models: []string{"m1", "m2"}, ModelContexts: map[string]int{"m1": claudeContext1M, "m2": 200_000},
		DefaultModel: "m1",
	}); err != nil {
		t.Fatalf("AddProvider() error = %v", err)
	}
	home := t.TempDir()
	sm := NewSwitchManager(pm, "", home)
	configPath := claudeCodeAdapter().ConfigPath(home)
	writeClaudeSettings(t, configPath, map[string]any{
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "gone",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "gone",
	})
	out, err := sm.Switch("claude-code", "main", nil, "", "", CredentialModeInline)
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	want := "m1" + claude1MSuffix
	env := readClaudeEnv(t, configPath)
	joined := strings.Join(out.Warnings, "\n")
	for _, key := range claudeTierModelKeys {
		if got := env[key]; got != want {
			t.Fatalf("%s = %v, want %q", key, got, want)
		}
		if !strings.Contains(joined, key) {
			t.Fatalf("warnings = %v, want %s", out.Warnings, key)
		}
	}
}

// SwitchWithOptions：档案声明 opus_model / sonnet_model 与单次覆盖的优先级、
// 1M 模型的 [1m] 后缀投影，以及不在模型集内时的 fail-closed 校验。
func TestClaudeCodeTierModelProjectionAndOverride(t *testing.T) {
	pm, _, _ := newTestProviderManager(t)
	if _, err := pm.AddProvider(AddProviderOptions{
		Alias: "zhipu", BaseURL: "https://open.bigmodel.cn/api/anthropic", APIKey: "sk-secret",
		Models:        []string{"glm-5.4", "glm-5.3-flash", "glm-5.4-mini"},
		ModelContexts: map[string]int{"glm-5.4": claudeContext1M, "glm-5.3-flash": 200_000, "glm-5.4-mini": 200_000},
		DefaultModel:  "glm-5.4",
		OpusModel:     "glm-5.4",
		SonnetModel:   "glm-5.3-flash",
	}); err != nil {
		t.Fatalf("AddProvider() error = %v", err)
	}
	home := t.TempDir()
	sm := NewSwitchManager(pm, "", home)
	configPath := claudeCodeAdapter().ConfigPath(home)

	// 1) 走档案声明：glm-5.4 声明 1M 应带 [1m]，glm-5.3-flash 原样写入。
	out, err := sm.SwitchWithOptions(SwitchOptions{
		AgentID:        "claude-code",
		ProviderAlias:  "zhipu",
		CredentialMode: CredentialModeInline,
	})
	if err != nil {
		t.Fatalf("SwitchWithOptions(profile) error = %v", err)
	}
	if out.OpusModel != "glm-5.4[1m]" || out.SonnetModel != "glm-5.3-flash" {
		t.Fatalf("out tiers = (%q, %q), want (glm-5.4[1m], glm-5.3-flash)", out.OpusModel, out.SonnetModel)
	}
	env := readClaudeEnv(t, configPath)
	if env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "glm-5.4[1m]" || env["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "glm-5.3-flash" {
		t.Fatalf("settings env tiers = (%v, %v)", env["ANTHROPIC_DEFAULT_OPUS_MODEL"], env["ANTHROPIC_DEFAULT_SONNET_MODEL"])
	}

	// 2) 单次覆盖优先于档案声明，且不回写档案。
	out, err = sm.SwitchWithOptions(SwitchOptions{
		AgentID:        "claude-code",
		ProviderAlias:  "zhipu",
		OpusModel:      "glm-5.4-mini",
		SonnetModel:    "glm-5.4",
		CredentialMode: CredentialModeInline,
	})
	if err != nil {
		t.Fatalf("SwitchWithOptions(override) error = %v", err)
	}
	if out.OpusModel != "glm-5.4-mini" || out.SonnetModel != "glm-5.4[1m]" {
		t.Fatalf("override out tiers = (%q, %q)", out.OpusModel, out.SonnetModel)
	}
	env = readClaudeEnv(t, configPath)
	if env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "glm-5.4-mini" || env["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "glm-5.4[1m]" {
		t.Fatalf("override settings env = (%v, %v)", env["ANTHROPIC_DEFAULT_OPUS_MODEL"], env["ANTHROPIC_DEFAULT_SONNET_MODEL"])
	}
	entry, err := pm.GetProvider("zhipu")
	if err != nil || entry.OpusModel != "glm-5.4" || entry.SonnetModel != "glm-5.3-flash" {
		t.Fatalf("profile mutated: (%q, %q, %v)", entry.OpusModel, entry.SonnetModel, err)
	}

	// 3) 不在模型集内的显式值 fail-closed。
	if _, err := sm.SwitchWithOptions(SwitchOptions{
		AgentID:        "claude-code",
		ProviderAlias:  "zhipu",
		OpusModel:      "ghost",
		CredentialMode: CredentialModeInline,
	}); err == nil || !strings.Contains(err.Error(), "opus model") {
		t.Fatalf("ghost opus err = %v, want opus model error", err)
	}
	if _, err := sm.SwitchWithOptions(SwitchOptions{
		AgentID:        "claude-code",
		ProviderAlias:  "zhipu",
		SonnetModel:    "ghost",
		CredentialMode: CredentialModeInline,
	}); err == nil || !strings.Contains(err.Error(), "sonnet model") {
		t.Fatalf("ghost sonnet err = %v, want sonnet model error", err)
	}
}
