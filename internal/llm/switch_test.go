package llm

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestUpsertTOMLTopLevelAndBlock(t *testing.T) {
	src := `# 顶部注释
model = "old"
model_provider = "minimax"
model_context_window = 1000

[model_providers.minimax]
name = "MiniMax"
base_url = "https://api.minimaxi.com/v1"

[mcp_servers.senv]
command = "senv"
`
	lines := strings.Split(src, "\n")
	lines = upsertTopLevelLine(lines, "model", `model = "new"`)
	lines = upsertTopLevelLine(lines, "model_provider", `model_provider = "senv-main"`)
	lines = upsertTOMLBlock(lines,
		"[model_providers.senv-main]",
		"[model_providers.senv-main]\nname = \"senv main\"\nbase_url = \"https://x\"\n")
	out := strings.Join(lines, "\n")
	if !strings.Contains(out, "model = \"new\"") || !strings.Contains(out, "model_provider = \"senv-main\"") {
		t.Fatalf("top-level edits missing:\n%s", out)
	}
	if strings.Contains(out, `model = "old"`) || strings.Contains(out, `model_provider = "minimax"`) {
		t.Fatalf("old top-level lines kept:\n%s", out)
	}
	if !strings.Contains(out, "# 顶部注释") || !strings.Contains(out, "model_context_window = 1000") {
		t.Fatalf("comments/unknown keys lost:\n%s", out)
	}
	if !strings.Contains(out, "[model_providers.minimax]") {
		t.Fatalf("unrelated block lost:\n%s", out)
	}
	// senv 块追加在文件末尾（minimax 块之后），既有 mcp 块保留在其前。
	senvIdx := strings.Index(out, "[model_providers.senv-main]")
	minimaxIdx := strings.Index(out, "[model_providers.minimax]")
	mcpIdx := strings.Index(out, "[mcp_servers.senv]")
	if !(senvIdx > minimaxIdx && mcpIdx >= 0 && mcpIdx < senvIdx) {
		t.Fatalf("block order wrong: minimax@%d senv@%d mcp@%d\n%s", minimaxIdx, senvIdx, mcpIdx, out)
	}
	// 重复 upsert 不产生重复块。
	lines = upsertTOMLBlock(strings.Split(out, "\n"), "[model_providers.senv-main]",
		"[model_providers.senv-main]\nname = \"updated\"\n")
	out2 := strings.Join(lines, "\n")
	if strings.Count(out2, "[model_providers.senv-main]") != 1 {
		t.Fatalf("duplicate block after re-upsert:\n%s", out2)
	}
	if !strings.Contains(out2, `name = "updated"`) {
		t.Fatalf("block not replaced:\n%s", out2)
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
		Model: "m1", Credential: credential, ConfigPath: configPath,
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
	if strings.Contains(out, "sk-secret") {
		t.Fatal("plaintext key leaked into codex config")
	}
	for _, want := range []string{
		`model = "m1"`, `model_provider = "senv-main"`,
		`env_key = "SENV_MAIN_API_KEY"`, "requires_openai_auth = false",
		`base_url = "https://api.example.com"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("codex config missing %q:\n%s", want, out)
		}
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
	for _, want := range []string{
		`default_model = "senv-main/m1"`,
		`[providers."senv-main"]`,
		`base_url = "https://api.example.com"`,
		`api_key = "sk-secret"`,
		`[models."senv-main/m1"]`,
		`provider = "senv-main"`,
		`model = "m1"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("kimi config missing %q:\n%s", want, out)
		}
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
	out, err := sm.Switch("claude-code", "main", "")
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	// 省略 --model 时取 default_model。
	if out.Model != "m1" {
		t.Fatalf("Model = %q, want m1", out.Model)
	}
	// 指针落盘且不含凭据。
	pf, err := LoadPointers(DefaultPointerPath(home))
	if err != nil {
		t.Fatalf("LoadPointers() error = %v", err)
	}
	p, ok := pf.Get("claude-code")
	if !ok || p.Provider != "main" || p.Model != "m1" {
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
	if _, err := sm.Switch("cursor", "main", "m1"); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("Switch(cursor) error = %v", err)
	}
	if _, err := sm.Switch("claude-code", "missing", "m1"); err == nil {
		t.Fatal("Switch(missing provider) unexpectedly succeeded")
	}
	if _, err := sm.Switch("claude-code", "main", "nope"); err == nil || !strings.Contains(err.Error(), "available") {
		t.Fatalf("Switch(bad model) error = %v", err)
	}
}

func TestSwitchCodexGuidesEnvVar(t *testing.T) {
	sm, _ := newTestSwitchManager(t)
	out, err := sm.Switch("codex", "main", "m2")
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
	if _, err := sm.Switch("claude-code", "main", "m1"); err == nil {
		t.Fatal("Switch() unexpectedly succeeded with broken pointer path")
	}
	if got := mustRead(t, configPath); string(got) != string(original) {
		t.Fatalf("config not restored: %s", got)
	}
	if _, err := os.Stat(configPath + ".senv-bak"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup left behind: %v", err)
	}
}

func TestStatusMixed(t *testing.T) {
	sm, _ := newTestSwitchManager(t)
	if _, err := sm.Switch("opencode", "main", "m1"); err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	rows, warning := sm.Status()
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
