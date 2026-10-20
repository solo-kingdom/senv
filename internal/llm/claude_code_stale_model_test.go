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
