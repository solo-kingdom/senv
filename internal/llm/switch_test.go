package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/wii/senv/internal/storage"
)

func TestApplyJSONMergePreservesUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"theme":"dark","custom":{"keep":true}}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := applyJSONMerge(path, func(root map[string]any) error {
		root["model"] = "m1"
		return nil
	}); err != nil {
		t.Fatalf("applyJSONMerge() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("perm = %o, want 600", got)
	}
	var root map[string]any
	if err := json.Unmarshal(mustRead(t, path), &root); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if root["theme"] != "dark" || root["model"] != "m1" {
		t.Fatalf("root = %v, want theme dark + model m1", root)
	}
	custom, ok := root["custom"].(map[string]any)
	if !ok || custom["keep"] != true {
		t.Fatalf("custom = %v, want keep:true", root["custom"])
	}
	if _, err := os.Stat(path + ".senv-bak"); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
}

func TestAtomicWriteKeepsOldOnTempFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	// 把目录换成普通文件使 MkdirAll 失败。
	blocker := filepath.Join(dir, "block")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := atomicWriteWithBackup(filepath.Join(blocker, "x.json"), []byte("{}")); err == nil {
		t.Fatal("atomicWriteWithBackup() unexpectedly succeeded")
	}
	if got := mustRead(t, path); string(got) != `{"a":1}` {
		t.Fatalf("config changed after failure: %s", got)
	}
}

func TestTOMLMergePreservesSemantics(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	src := `# user comment
model = "old"

[[items]]
name = "keep"

[mcp_servers.senv]
command = "senv"
`
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	err := applyTOMLMerge(path, func(root map[string]any) error {
		root["model"] = "new"
		root["model_provider"] = "senv-main"
		setTOMLPath(root, []string{"model_providers", "senv-main"}, map[string]any{
			"name":     "senv main",
			"base_url": "https://x",
		})
		return nil
	})
	if err != nil {
		t.Fatalf("applyTOMLMerge() error = %v", err)
	}
	var root map[string]any
	if err := toml.Unmarshal(mustRead(t, path), &root); err != nil {
		t.Fatalf("TOML round-trip: %v", err)
	}
	if root["model"] != "new" || root["model_provider"] != "senv-main" {
		t.Fatalf("root = %v", root)
	}
	items, ok := root["items"].([]any)
	if !ok || len(items) != 1 || items[0].(map[string]any)["name"] != "keep" {
		t.Fatalf("array of tables = %#v", root["items"])
	}
	if _, ok := root["mcp_servers"].(map[string]any)["senv"]; !ok {
		t.Fatal("unrelated table lost")
	}
	var count int
	for _, line := range strings.Split(string(mustRead(t, path)), "\n") {
		if strings.HasPrefix(line, "model = ") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("model top-level key count = %d", count)
	}
}

func TestProviderIDEscaping(t *testing.T) {
	if got := senvProviderID("My Prov.1"); got != "senv-My-Prov-1" {
		t.Fatalf("senvProviderID = %q", got)
	}
	if got := senvEnvKeyName("My-Prov"); got != "SENV_MY_PROV_API_KEY" {
		t.Fatalf("senvEnvKeyName = %q", got)
	}
}

// applyAdapter 以 tmp home 跑一个适配器并返回配置内容。
func applyAdapter(t *testing.T, a AgentAdapter, credential string) (string, string) {
	t.Helper()
	home := t.TempDir()
	configPath := a.ConfigPath(home)
	req := SwitchRequest{
		AgentID: a.ID, ProviderAlias: "main", BaseURL: "https://api.example.com",
		Models: []string{"m1"}, DefaultModel: "m1", Credential: credential, ConfigPath: configPath,
		Home: home,
	}
	if err := a.Apply(req); err != nil {
		t.Fatalf("%s Apply() error = %v", a.ID, err)
	}
	return string(mustRead(t, configPath)), configPath
}

func TestClaudeCodeAdapter(t *testing.T) {
	out, _ := applyAdapter(t, claudeCodeAdapter(), "sk-secret")
	var root map[string]any
	if err := json.Unmarshal([]byte(out), &root); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if root["model"] != "m1" {
		t.Fatalf("model = %v", root["model"])
	}
	env := root["env"].(map[string]any)
	if env["ANTHROPIC_BASE_URL"] != "https://api.example.com" || env["ANTHROPIC_AUTH_TOKEN"] != "sk-secret" {
		t.Fatalf("env = %v", env)
	}
}

func TestCodexAdapterNoSecretOnDisk(t *testing.T) {
	out, path := applyAdapter(t, codexAdapter(), senvEnvKeyName("main"))
	var cfg map[string]any
	if err := toml.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("codex TOML parse: %v", err)
	}
	if strings.Contains(out, "sk-secret") {
		t.Fatal("plaintext key leaked into codex config")
	}
	if cfg["model"] != "m1" || cfg["model_provider"] != "senv-main" {
		t.Fatalf("codex top-level = %v", cfg)
	}
	provider := cfg["model_providers"].(map[string]any)["senv-main"].(map[string]any)
	if provider["env_key"] != "SENV_MAIN_API_KEY" || provider["base_url"] != "https://api.example.com" || provider["requires_openai_auth"] != false {
		t.Fatalf("codex provider = %v", provider)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("perm = %o, want 600", got)
	}
}

func TestKimiAdapter(t *testing.T) {
	out, _ := applyAdapter(t, kimiAdapter(), "sk-secret")
	var cfg map[string]any
	if err := toml.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("kimi TOML parse: %v", err)
	}
	if cfg["default_model"] != "senv-main/m1" {
		t.Fatalf("default_model = %v", cfg["default_model"])
	}
	provider := cfg["providers"].(map[string]any)["senv-main"].(map[string]any)
	model := cfg["models"].(map[string]any)["senv-main/m1"].(map[string]any)
	if provider["base_url"] != "https://api.example.com" || provider["api_key"] != "sk-secret" {
		t.Fatalf("kimi provider = %v", provider)
	}
	if model["provider"] != "senv-main" || model["model"] != "m1" {
		t.Fatalf("kimi model = %v", model)
	}
}

func TestPiAdapterWritesBothFiles(t *testing.T) {
	a := piAdapter()
	out, configPath := applyAdapter(t, a, "sk-secret")
	var models map[string]any
	if err := json.Unmarshal([]byte(out), &models); err != nil {
		t.Fatalf("Unmarshal(models.json) error = %v", err)
	}
	prov := models["providers"].(map[string]any)["senv-main"].(map[string]any)
	if prov["baseUrl"] != "https://api.example.com" || prov["apiKey"] != "sk-secret" || prov["api"] != "openai-completions" {
		t.Fatalf("pi provider = %v", prov)
	}
	list := prov["models"].([]any)
	entry := list[0].(map[string]any)
	if entry["id"] != "m1" {
		t.Fatalf("pi model entry = %v", entry)
	}
	settings := string(mustRead(t, filepath.Join(filepath.Dir(configPath), "settings.json")))
	var sroot map[string]any
	if err := json.Unmarshal([]byte(settings), &sroot); err != nil {
		t.Fatalf("Unmarshal(settings.json) error = %v", err)
	}
	if sroot["defaultProvider"] != "senv-main" || sroot["defaultModel"] != "m1" {
		t.Fatalf("pi settings = %v", sroot)
	}
}

func TestOpencodeAdapter(t *testing.T) {
	out, _ := applyAdapter(t, opencodeAdapter(), "sk-secret")
	var root map[string]any
	if err := json.Unmarshal([]byte(out), &root); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if root["model"] != "senv-main/m1" {
		t.Fatalf("model = %v", root["model"])
	}
	prov := root["provider"].(map[string]any)["senv-main"].(map[string]any)
	if prov["npm"] != "@ai-sdk/openai-compatible" {
		t.Fatalf("npm = %v", prov["npm"])
	}
	opts := prov["options"].(map[string]any)
	if opts["baseURL"] != "https://api.example.com" || opts["apiKey"] != "sk-secret" {
		t.Fatalf("options = %v", opts)
	}
}

func newTestSwitchManager(t *testing.T) (*SwitchManager, string) {
	t.Helper()
	pm, _, _ := newTestProviderManager(t)
	if _, err := pm.AddProvider(AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-secret", Models: []string{"m1", "m2"}, DefaultModel: "m1",
	}); err != nil {
		t.Fatalf("AddProvider() error = %v", err)
	}
	home := t.TempDir()
	return NewSwitchManager(pm, "", home), home
}

func TestSwitchEndToEnd(t *testing.T) {
	sm, home := newTestSwitchManager(t)
	out, err := sm.Switch("claude-code", "main", nil, "")
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	// 省略 --model 时取 default_model。
	if out.DefaultModel != "m1" {
		t.Fatalf("DefaultModel = %q, want m1", out.DefaultModel)
	}
	// 省略模型集时取 Provider 模型集全集。
	if len(out.Models) != 2 || out.Models[0] != "m1" || out.Models[1] != "m2" {
		t.Fatalf("Models = %v, want [m1 m2]", out.Models)
	}
	// 指针落盘且不含凭据。
	pf, err := LoadPointers(DefaultPointerPath(home))
	if err != nil {
		t.Fatalf("LoadPointers() error = %v", err)
	}
	p, ok := pf.Get("claude-code")
	if !ok || p.Provider != "main" || p.DefaultModel != "m1" || len(p.Models) != 2 {
		t.Fatalf("pointer = %+v", p)
	}
	pointerRaw := string(mustRead(t, DefaultPointerPath(home)))
	if strings.Contains(pointerRaw, "sk-secret") {
		t.Fatal("pointer file contains credential")
	}
	// agent 配置包含解密后的凭据。
	cfg := string(mustRead(t, out.ConfigPath))
	if !strings.Contains(cfg, "sk-secret") {
		t.Fatal("agent config missing decrypted credential")
	}
}

func TestSwitchValidationFailures(t *testing.T) {
	sm, _ := newTestSwitchManager(t)
	if _, err := sm.Switch("cursor", "main", []string{"m1"}, "m1"); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("Switch(cursor) error = %v", err)
	}
	if _, err := sm.Switch("claude-code", "missing", []string{"m1"}, "m1"); err == nil {
		t.Fatal("Switch(missing provider) unexpectedly succeeded")
	}
	if _, err := sm.Switch("claude-code", "main", []string{"nope"}, "nope"); err == nil || !strings.Contains(err.Error(), "available") {
		t.Fatalf("Switch(bad model) error = %v", err)
	}
}

func TestSwitchCodexGuidesEnvVar(t *testing.T) {
	sm, _ := newTestSwitchManager(t)
	out, err := sm.Switch("codex", "main", []string{"m2"}, "m2")
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if out.CredentialEnv != "SENV_MAIN_API_KEY" {
		t.Fatalf("CredentialEnv = %q", out.CredentialEnv)
	}
	if strings.Contains(string(mustRead(t, out.ConfigPath)), "sk-secret") {
		t.Fatal("codex config contains plaintext key")
	}
}

func TestSwitchRollsBackOnPointerFailure(t *testing.T) {
	pm, _, _ := newTestProviderManager(t)
	if _, err := pm.AddProvider(AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-secret", Models: []string{"m1"},
	}); err != nil {
		t.Fatalf("AddProvider() error = %v", err)
	}
	home := t.TempDir()
	configPath := claudeCodeAdapter().ConfigPath(home)
	original := []byte(`{"theme":"dark"}`)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	// 指针路径挂在普通文件下，SavePointers 必败。
	badPointer := filepath.Join(home, "blocker", "pointers.json")
	if err := os.WriteFile(filepath.Join(home, "blocker"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	sm := NewSwitchManager(pm, badPointer, home)
	if _, err := sm.Switch("claude-code", "main", []string{"m1"}, "m1"); err == nil {
		t.Fatal("Switch() unexpectedly succeeded with broken pointer path")
	}
	if got := mustRead(t, configPath); string(got) != string(original) {
		t.Fatalf("config not restored: %s", got)
	}
	if matches, _ := filepath.Glob(configPath + ".senv-bak*"); len(matches) != 0 {
		t.Fatalf("backups left behind: %v", matches)
	}
}

func TestStatusMixed(t *testing.T) {
	sm, _ := newTestSwitchManager(t)
	if _, err := sm.Switch("opencode", "main", []string{"m1"}, "m1"); err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	rows, warning, err := sm.Status()
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if warning != "" {
		t.Fatalf("unexpected warning %q", warning)
	}
	byID := map[string]StatusRow{}
	for _, r := range rows {
		byID[r.AgentID] = r
	}
	oc := byID["opencode"]
	if !oc.Supported || oc.Pointer == nil || oc.Pointer.Provider != "main" {
		t.Fatalf("opencode row = %+v", oc)
	}
	cc := byID["claude-code"]
	if !cc.Supported || cc.Pointer != nil {
		t.Fatalf("claude-code row = %+v", cc)
	}
	zc := byID["zcode"]
	if zc.Supported {
		t.Fatalf("zcode should be unsupported: %+v", zc)
	}
	cur := byID["cursor"]
	if cur.Supported || cur.ConfigPath == "" {
		t.Fatalf("cursor row = %+v", cur)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return data
}

// TestSwitchBaseURLPerProtocolFamily 覆盖存量档案（直接落库、未经过 add 归一）
// 在切换时按 agent 协议族转换：Anthropic 族剥离末段 /v1，OpenAI 兼容族保持带
// 版本形态，且重复切换幂等。
func TestSwitchBaseURLPerProtocolFamily(t *testing.T) {
	cases := []struct {
		name       string
		stored     string
		claudeCode string
		codex      string
	}{
		{"存量档案缺版本段", "https://api.example.com",
			"https://api.example.com", "https://api.example.com/v1"},
		{"存量档案带版本段", "https://api.example.com/v1",
			"https://api.example.com", "https://api.example.com/v1"},
		{"存量档案带尾斜杠", "https://api.example.com/v1/",
			"https://api.example.com", "https://api.example.com/v1"},
		{"带路径前缀", "https://api.example.com/api/llm/v1",
			"https://api.example.com/api/llm", "https://api.example.com/api/llm/v1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pm, _, _ := newTestProviderManager(t)
			if err := pm.save("legacy", &storage.LLMProviderEntry{
				Alias: "legacy", BaseURL: tc.stored,
				CredentialRef: "text:llm-keys/legacy", Models: []string{"m1"},
			}); err != nil {
				t.Fatalf("save legacy provider: %v", err)
			}
			if err := pm.textManager().Set(LLMKeysGroup, "legacy", "sk-secret"); err != nil {
				t.Fatalf("store credential: %v", err)
			}
			home := t.TempDir()
			sm := NewSwitchManager(pm, "", home)

			out, err := sm.Switch("claude-code", "legacy", nil, "")
			if err != nil {
				t.Fatalf("Switch(claude-code) error = %v", err)
			}
			if out.BaseURL != tc.claudeCode {
				t.Fatalf("claude-code BaseURL = %q, want %q", out.BaseURL, tc.claudeCode)
			}
			var claudeCfg map[string]any
			if err := json.Unmarshal([]byte(string(mustRead(t, out.ConfigPath))), &claudeCfg); err != nil {
				t.Fatalf("parse claude-code config: %v", err)
			}
			if got := claudeCfg["env"].(map[string]any)["ANTHROPIC_BASE_URL"]; got != tc.claudeCode {
				t.Fatalf("ANTHROPIC_BASE_URL = %v, want %q", got, tc.claudeCode)
			}
			// 重复切换幂等：同一档案再切一次，写入值不变。
			again, err := sm.Switch("claude-code", "legacy", nil, "")
			if err != nil {
				t.Fatalf("second Switch(claude-code) error = %v", err)
			}
			if again.BaseURL != out.BaseURL {
				t.Fatalf("repeated switch BaseURL = %q, want %q", again.BaseURL, out.BaseURL)
			}

			codexOut, err := sm.Switch("codex", "legacy", nil, "")
			if err != nil {
				t.Fatalf("Switch(codex) error = %v", err)
			}
			if codexOut.BaseURL != tc.codex {
				t.Fatalf("codex BaseURL = %q, want %q", codexOut.BaseURL, tc.codex)
			}
			var codexCfg map[string]any
			if err := toml.Unmarshal(mustRead(t, codexOut.ConfigPath), &codexCfg); err != nil {
				t.Fatalf("parse codex config: %v", err)
			}
			provider := codexCfg["model_providers"].(map[string]any)["senv-legacy"].(map[string]any)
			if provider["base_url"] != tc.codex {
				t.Fatalf("codex base_url = %v, want %q", provider["base_url"], tc.codex)
			}
		})
	}
}

// TestAddProviderNormalizesBaseURL 覆盖写入侧归一与改写提示。
func TestAddProviderNormalizesBaseURL(t *testing.T) {
	cases := []struct {
		name        string
		input       string
		wantStored  string
		wantWarning bool
	}{
		{"缺版本段被补齐", "https://api.example.com", "https://api.example.com/v1", true},
		{"尾斜杠被收敛", "https://api.example.com/v1/", "https://api.example.com/v1", true},
		{"已归一静默通过", "https://api.example.com/v1", "https://api.example.com/v1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pm, _, _ := newTestProviderManager(t)
			res, err := pm.AddProvider(AddProviderOptions{
				Alias: "main", BaseURL: tc.input, APIKey: "sk-secret", Models: []string{"m1"},
			})
			if err != nil {
				t.Fatalf("AddProvider() error = %v", err)
			}
			if res.Entry.BaseURL != tc.wantStored {
				t.Fatalf("stored BaseURL = %q, want %q", res.Entry.BaseURL, tc.wantStored)
			}
			gotWarning := len(res.Warnings) > 0
			if gotWarning != tc.wantWarning {
				t.Fatalf("warnings = %v, want warning=%v", res.Warnings, tc.wantWarning)
			}
			if tc.wantWarning && !strings.Contains(res.Warnings[0], tc.wantStored) {
				t.Fatalf("warning %q does not mention %q", res.Warnings[0], tc.wantStored)
			}
		})
	}
}
