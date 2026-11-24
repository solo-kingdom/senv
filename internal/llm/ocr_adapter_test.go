package llm

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wii/senv/internal/env"
)

func TestLookupAgentAcceptsOCRAliases(t *testing.T) {
	for _, id := range []string{"ocr", "OCR", "open-code-review", "Open-Code-Review"} {
		a, ok := LookupAgent(id)
		if !ok || a.ID != "ocr" {
			t.Fatalf("LookupAgent(%q) = %q, %v; want ocr", id, a.ID, ok)
		}
	}
	for _, a := range SupportedAgents() {
		if a.ID == "open-code-review" {
			t.Fatal("SupportedAgents must list canonical ids only")
		}
	}
}

func TestOCRAdapterWritesCustomProviderAndPreservesUnrelatedKeys(t *testing.T) {
	home := t.TempDir()
	a := ocrAdapter()
	if a.ID != "ocr" || a.Name != "Open Code Review" {
		t.Fatalf("adapter identity = %q/%q", a.ID, a.Name)
	}
	cfgPath := filepath.Join(home, ".opencodereview", "config.json")
	if got := a.ConfigPath(home); got != cfgPath {
		t.Fatalf("ConfigPath = %q, want %q", got, cfgPath)
	}
	writeFileAt(t, cfgPath, `{
		"language": "zh-CN",
		"effort": "high",
		"max_tokens": 8192,
		"providers": {
			"anthropic": {"api_key": "keep-built-in"}
		},
		"custom_providers": {
			"user-own": {"url": "https://own.example.com/v1", "protocol": "openai", "api_key": "k-own", "model": "m-own"},
			"senv-old": {"url": "https://old.example.com/v1", "protocol": "openai", "api_key": "k-old", "model": "m-old"}
		},
		"mcp_servers": {
			"github": {"command": "npx"}
		}
	}`)

	req := SwitchRequest{
		AgentID:        a.ID,
		ProviderAlias:  "main",
		BaseURL:        "https://api.example.com/v1",
		Models:         []string{"m1", "m2"},
		DefaultModel:   "m2",
		Credential:     "printenv SENV_MAIN_API_KEY",
		CredentialMode: CredentialEnvVar,
		ConfigPath:     cfgPath,
		Home:           home,
		PriorProvider:  "old",
	}
	if err := a.Apply(req); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	root := readJSONAt(t, cfgPath)
	if root["provider"] != "senv-main" || root["model"] != "m2" {
		t.Fatalf("top-level provider/model = %v / %v", root["provider"], root["model"])
	}
	if root["language"] != "zh-CN" || root["effort"] != "high" || root["max_tokens"] != float64(8192) {
		t.Fatalf("unrelated top-level keys clobbered: %v", root)
	}
	if _, ok := root["mcp_servers"].(map[string]any)["github"]; !ok {
		t.Fatalf("mcp_servers clobbered: %v", root["mcp_servers"])
	}
	if _, ok := root["providers"].(map[string]any)["anthropic"]; !ok {
		t.Fatalf("built-in providers clobbered: %v", root["providers"])
	}

	custom := root["custom_providers"].(map[string]any)
	if _, hasOld := custom["senv-old"]; hasOld {
		t.Fatalf("prior senv provider must be removed: %v", custom)
	}
	if _, hasOwn := custom["user-own"]; !hasOwn {
		t.Fatalf("user-owned custom provider must be preserved: %v", custom)
	}
	entry := custom["senv-main"].(map[string]any)
	if entry["url"] != "https://api.example.com/v1" || entry["protocol"] != "openai" || entry["model"] != "m2" {
		t.Fatalf("entry = %v", entry)
	}
	if entry["api_key_cmd"] != "printenv SENV_MAIN_API_KEY" {
		t.Fatalf("api_key_cmd = %v", entry["api_key_cmd"])
	}
	if _, hasKey := entry["api_key"]; hasKey {
		t.Fatalf("api_key must not coexist with api_key_cmd: %v", entry)
	}
	models := entry["models"].([]any)
	if len(models) != 2 || models[0] != "m1" || models[1] != "m2" {
		t.Fatalf("models = %v, want [m1 m2]", models)
	}
}

func TestOCRAdapterMultiProtocolAndShapeURLs(t *testing.T) {
	cases := []struct {
		name         string
		opts         AddProviderOptions
		wantProtocol string
		wantURL      string
	}{
		{
			name: "undeclared shape defaults to openai with BaseURL",
			opts: AddProviderOptions{
				Alias: "p-default", BaseURL: "https://gw.example.com",
				APIKey: "sk-1", Models: []string{"m1"},
			},
			wantProtocol: "openai",
			wantURL:      "https://gw.example.com/v1",
		},
		{
			name: "openai-chat prefers chat_base_url",
			opts: AddProviderOptions{
				Alias: "p-chat", BaseURL: "https://gw.example.com",
				ShapeURLs: map[string]string{"openai-chat": "https://chat.example.com/v1"},
				APIShape:  "openai-chat",
				APIKey:    "sk-2", Models: []string{"m1"},
			},
			wantProtocol: "openai",
			wantURL:      "https://chat.example.com/v1",
		},
		{
			name: "openai-responses prefers responses_base_url",
			opts: AddProviderOptions{
				Alias: "p-resp", BaseURL: "https://gw.example.com",
				ShapeURLs: map[string]string{"openai-responses": "https://resp.example.com/v1"},
				APIShape:  "openai-responses",
				APIKey:    "sk-3", Models: []string{"m1"},
			},
			wantProtocol: "openai-responses",
			wantURL:      "https://resp.example.com/v1",
		},
		{
			name: "anthropic prefers anthropic_base_url verbatim",
			opts: AddProviderOptions{
				Alias: "p-ant-explicit", BaseURL: "https://gw.example.com",
				ShapeURLs: map[string]string{"anthropic": "https://gw.example.com/api/anthropic"},
				APIShape:  "anthropic",
				APIKey:    "sk-4", Models: []string{"m1"},
			},
			wantProtocol: "anthropic",
			wantURL:      "https://gw.example.com/api/anthropic",
		},
		{
			name: "anthropic falls back to BaseURL with /v1 stripped",
			opts: AddProviderOptions{
				Alias: "p-ant-fallback", BaseURL: "https://api.anthropic.com/v1",
				APIShape: "anthropic",
				APIKey:   "sk-5", Models: []string{"m1"},
			},
			wantProtocol: "anthropic",
			wantURL:      "https://api.anthropic.com",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pm, _, _ := newTestProviderManager(t)
			addTestProvider(t, pm, tc.opts)

			home := t.TempDir()
			out, err := NewSwitchManager(pm, "", home).Switch("ocr", tc.opts.Alias, nil, "", "", CredentialModeAuto)
			if err != nil {
				t.Fatalf("Switch() error = %v", err)
			}
			if out.BaseURL != tc.wantURL {
				t.Fatalf("out.BaseURL = %q, want %q", out.BaseURL, tc.wantURL)
			}
			root := readJSONAt(t, out.ConfigPath)
			entry := root["custom_providers"].(map[string]any)[senvProviderID(tc.opts.Alias)].(map[string]any)
			if entry["protocol"] != tc.wantProtocol {
				t.Fatalf("protocol = %v, want %q", entry["protocol"], tc.wantProtocol)
			}
			if entry["url"] != tc.wantURL {
				t.Fatalf("url = %v, want %q", entry["url"], tc.wantURL)
			}
		})
	}
}

func TestOCRAdapterCredentialModesAndMutualExclusion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("printenv env-mode requires non-Windows")
	}
	pm, store, _ := newTestProviderManager(t)
	addTestProvider(t, pm, AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-secret", Models: []string{"m1"},
	})

	home := t.TempDir()
	sm := NewSwitchManager(pm, "", home)

	// 1. auto 模式：写 api_key_cmd 且无 api_key，senv env export 可提供。
	out, err := sm.Switch("ocr", "main", nil, "", "", CredentialModeAuto)
	if err != nil {
		t.Fatalf("Switch(auto) error = %v", err)
	}
	if out.CredentialEnv == "" {
		t.Fatal("CredentialEnv = \"\", want env var name under auto mode")
	}
	cfg := string(mustRead(t, out.ConfigPath))
	if strings.Contains(cfg, "sk-secret") {
		t.Fatalf("auto mode leaked plaintext key:\n%s", cfg)
	}
	entry := readJSONAt(t, out.ConfigPath)["custom_providers"].(map[string]any)["senv-main"].(map[string]any)
	if entry["api_key_cmd"] != "printenv "+out.CredentialEnv {
		t.Fatalf("api_key_cmd = %v, want printenv %s", entry["api_key_cmd"], out.CredentialEnv)
	}
	if _, hasKey := entry["api_key"]; hasKey {
		t.Fatalf("auto mode must not write api_key: %v", entry)
	}
	exports, err := env.NewManager(store, "test-password").Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if !strings.Contains(exports, out.CredentialEnv) {
		t.Fatalf("env export lacks %s:\n%s", out.CredentialEnv, exports)
	}

	// 2. inline 模式：写 api_key 且清理 api_key_cmd。
	out, err = sm.Switch("ocr", "main", nil, "", "", CredentialModeInline)
	if err != nil {
		t.Fatalf("Switch(inline) error = %v", err)
	}
	if out.CredentialEnv != "" {
		t.Fatalf("inline CredentialEnv = %q, want empty", out.CredentialEnv)
	}
	entry = readJSONAt(t, out.ConfigPath)["custom_providers"].(map[string]any)["senv-main"].(map[string]any)
	if entry["api_key"] != "sk-secret" {
		t.Fatalf("api_key = %v, want sk-secret", entry["api_key"])
	}
	if _, hasCmd := entry["api_key_cmd"]; hasCmd {
		t.Fatalf("inline mode must not leave api_key_cmd: %v", entry)
	}

	// 3. 切回 env 模式：写 api_key_cmd 且清理 api_key。
	out, err = sm.Switch("ocr", "main", nil, "", "", CredentialModeEnv)
	if err != nil {
		t.Fatalf("Switch(env) error = %v", err)
	}
	entry = readJSONAt(t, out.ConfigPath)["custom_providers"].(map[string]any)["senv-main"].(map[string]any)
	if entry["api_key_cmd"] != "printenv "+out.CredentialEnv {
		t.Fatalf("api_key_cmd = %v", entry["api_key_cmd"])
	}
	if _, hasKey := entry["api_key"]; hasKey {
		t.Fatalf("env mode must not leave api_key: %v", entry)
	}
}

func TestOCRAdapterRejectsInvalidEnvVarInAPIKeyCmd(t *testing.T) {
	home := t.TempDir()
	a := ocrAdapter()
	cfgPath := a.ConfigPath(home)

	for _, bad := range []string{"sk-secret", "printenv ", "printenv BAD;id", "printenv BAD NAME"} {
		err := a.Apply(SwitchRequest{
			AgentID:        a.ID,
			ProviderAlias:  "main",
			BaseURL:        "https://api.example.com/v1",
			Models:         []string{"m1"},
			DefaultModel:   "m1",
			Credential:     bad,
			CredentialMode: CredentialEnvVar,
			ConfigPath:     cfgPath,
			Home:           home,
		})
		if err == nil {
			t.Fatalf("Apply(%q) expected error, got nil", bad)
		}
		if _, statErr := os.Stat(cfgPath); !os.IsNotExist(statErr) {
			t.Fatalf("failed Apply(%q) must not write %s", bad, cfgPath)
		}
	}
}
