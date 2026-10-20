package llm

import (
	"encoding/json"
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

// TestCredentialEnvModeClaudeCode 覆盖 claude-code 的 env 模式：settings.json
// 不写 ANTHROPIC_BASE_URL/AUTH_TOKEN（进程环境兜底），清理上次 inline 遗留。
func TestCredentialEnvModeClaudeCode(t *testing.T) {
	pm, store, _ := newTestProviderManager(t)
	addTestProvider(t, pm, AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-secret", Models: []string{"m1"},
	})
	envMgr := env.NewManager(store, "test-password")

	home := t.TempDir()
	// 先 inline 切换，制造 stale 明文。
	if _, err := NewSwitchManager(pm, "", home).Switch("claude-code", "main", nil, "", "", CredentialModeInline); err != nil {
		t.Fatalf("inline Switch() error = %v", err)
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	raw := string(mustRead(t, settingsPath))
	if !strings.Contains(raw, "sk-secret") {
		t.Fatalf("inline switch must write plaintext first:\n%s", raw)
	}

	// 再 auto 切换：env 模式清掉明文键，靠进程环境兜底。
	out, err := NewSwitchManager(pm, "", home).Switch("claude-code", "main", nil, "", "", CredentialModeAuto)
	if err != nil {
		t.Fatalf("auto Switch() error = %v", err)
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
	if out.CredentialEnv == "" {
		t.Fatal("CredentialEnv = \"\", want an env var name for claude-code env mode")
	}
	// 接入地址环境变量也必须由 senv env export 提供。
	if out.BaseURLEnv == "" {
		t.Fatal("BaseURLEnv = \"\", want a base URL env var for claude-code env mode")
	}
	exports, err := envMgr.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if !strings.Contains(exports, out.CredentialEnv) || !strings.Contains(exports, out.BaseURLEnv) {
		t.Fatalf("env export lacks %s / %s:\n%s", out.CredentialEnv, out.BaseURLEnv, exports)
	}
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
