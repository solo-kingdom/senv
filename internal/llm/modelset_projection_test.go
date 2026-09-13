package llm

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

// projectionMeta 覆盖三种元数据情形：完整（name/description/context/output/
// efforts）、只有 name、完全未知。
func projectionMeta() map[string]ModelMetadata {
	return map[string]ModelMetadata{
		"m1": {Name: "Model One", Description: "first model", ContextLimit: 300000, OutputLimit: 32000, ReasoningEfforts: []string{"low", "high"}, DefaultReasoning: "high"},
		"m2": {Name: "Model Two"},
		"m3": {},
	}
}

func applyProjection(t *testing.T, a AgentAdapter, home string, models []string, defaultModel string) {
	t.Helper()
	req := SwitchRequest{
		AgentID:       a.ID,
		ProviderAlias: "main",
		BaseURL:       "https://api.example.com",
		Models:        models,
		DefaultModel:  defaultModel,
		Credential:    "sk-secret",
		ConfigPath:    a.ConfigPath(home),
		Home:          home,
		ModelMetadata: projectionMeta(),
	}
	if a.Credential == CredentialEnvVar {
		req.Credential = senvEnvKeyName("main")
	}
	if err := a.Apply(req); err != nil {
		t.Fatalf("%s Apply() error = %v", a.ID, err)
	}
}

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(mustRead(t, path), &root); err != nil {
		t.Fatalf("parse JSON %s: %v", path, err)
	}
	return root
}

func readTOMLFile(t *testing.T, path string) map[string]any {
	t.Helper()
	var root map[string]any
	if err := toml.Unmarshal(mustRead(t, path), &root); err != nil {
		t.Fatalf("parse TOML %s: %v", path, err)
	}
	return root
}

func TestClaudeCodeProjectsModelPicker(t *testing.T) {
	home := t.TempDir()
	a := claudeCodeAdapter()
	applyProjection(t, a, home, []string{"m1", "m2", "m3"}, "m2")

	root := readJSONFile(t, a.ConfigPath(home))
	if root["model"] != "m2" {
		t.Fatalf("model = %v, want default m2", root["model"])
	}
	picker, ok := root["modelPicker"].(map[string]any)
	if !ok {
		t.Fatalf("modelPicker = %#v", root["modelPicker"])
	}
	if picker["replaceBuiltInOptions"] != true {
		t.Fatalf("replaceBuiltInOptions = %v, want true", picker["replaceBuiltInOptions"])
	}
	options, ok := picker["options"].([]any)
	if !ok || len(options) != 3 {
		t.Fatalf("options = %#v", picker["options"])
	}
	var models, labels []string
	for _, raw := range options {
		option := raw.(map[string]any)
		models = append(models, option["model"].(string))
		labels = append(labels, option["label"].(string))
	}
	if !slices.Equal(models, []string{"m1", "m2", "m3"}) {
		t.Fatalf("picker models = %v, want provider order", models)
	}
	if !slices.Equal(labels, []string{"Model One", "Model Two", "m3"}) {
		t.Fatalf("picker labels = %v", labels)
	}
	if got := options[0].(map[string]any)["description"]; got != "first model" {
		t.Fatalf("m1 description = %v", got)
	}
	if _, ok := options[2].(map[string]any)["description"]; ok {
		t.Fatal("unknown model should not carry a description key")
	}
}

func TestCodexProjectsModelCatalog(t *testing.T) {
	home := t.TempDir()
	a := codexAdapter()
	applyProjection(t, a, home, []string{"m1", "m2", "m3"}, "m2")

	cfg := readTOMLFile(t, a.ConfigPath(home))
	if cfg["model"] != "m2" || cfg["model_provider"] != "senv-main" {
		t.Fatalf("codex top-level = %v", cfg)
	}
	if cfg["model_catalog_json"] != "~/.codex/model-catalogs/senv-main.json" {
		t.Fatalf("model_catalog_json = %v", cfg["model_catalog_json"])
	}

	catalogPath := codexCatalogPath(home, "main")
	info, err := os.Stat(catalogPath)
	if err != nil {
		t.Fatalf("Stat(catalog) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("catalog perm = %o, want 600", got)
	}
	var catalog struct {
		Models []struct {
			Slug                     string `json:"slug"`
			DisplayName              string `json:"display_name"`
			Description              string `json:"description"`
			DefaultReasoningLevel    string `json:"default_reasoning_level"`
			SupportedReasoningLevels []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
			ContextWindow   int      `json:"context_window"`
			InputModalities []string `json:"input_modalities"`
		} `json:"models"`
	}
	if err := json.Unmarshal(mustRead(t, catalogPath), &catalog); err != nil {
		t.Fatalf("parse catalog: %v", err)
	}
	if len(catalog.Models) != 3 {
		t.Fatalf("catalog models = %d, want 3", len(catalog.Models))
	}
	first := catalog.Models[0]
	if first.Slug != "m1" || first.DisplayName != "Model One" || first.Description != "first model" {
		t.Fatalf("catalog[0] = %+v", first)
	}
	if len(first.SupportedReasoningLevels) != 2 || first.SupportedReasoningLevels[0].Effort != "low" {
		t.Fatalf("catalog[0] reasoning levels = %+v", first.SupportedReasoningLevels)
	}
	if first.DefaultReasoningLevel != "high" {
		t.Fatalf("catalog[0] default_reasoning_level = %q, want declared high (not first effort)", first.DefaultReasoningLevel)
	}
	if first.ContextWindow != 300000 {
		t.Fatalf("catalog[0] context_window = %d", first.ContextWindow)
	}
	unknown := catalog.Models[2]
	if unknown.Slug != "m3" || unknown.DisplayName != "m3" {
		t.Fatalf("catalog[2] = %+v, want id fallback", unknown)
	}
	if unknown.ContextWindow != 0 {
		t.Fatalf("catalog[2] context_window = %d, want omitted/0", unknown.ContextWindow)
	}
	if unknown.DefaultReasoningLevel != "none" || len(unknown.SupportedReasoningLevels) != 1 || unknown.SupportedReasoningLevels[0].Effort != "none" {
		t.Fatalf("catalog[2] should fall back to a single none reasoning level: %+v", unknown)
	}
	if !slices.Equal(unknown.InputModalities, []string{"text"}) {
		t.Fatalf("catalog[2] input_modalities = %v, want text template", unknown.InputModalities)
	}
}

func TestKimiProjectsModelSet(t *testing.T) {
	home := t.TempDir()
	a := kimiAdapter()
	applyProjection(t, a, home, []string{"m1", "m2", "m3"}, "m2")

	cfg := readTOMLFile(t, a.ConfigPath(home))
	if cfg["default_model"] != "senv-main/m2" {
		t.Fatalf("default_model = %v", cfg["default_model"])
	}
	models, ok := cfg["models"].(map[string]any)
	if !ok || len(models) != 3 {
		t.Fatalf("models = %#v, want every selected model", cfg["models"])
	}
	m1 := models["senv-main/m1"].(map[string]any)
	if m1["max_context_size"] != int64(300000) || m1["display_name"] != "Model One" {
		t.Fatalf("m1 = %v, want catalog context and name", m1)
	}
	if m1["max_output_size"] != int64(32000) {
		t.Fatalf("m1 max_output_size = %v, want catalog output limit", m1["max_output_size"])
	}
	caps := anySlice(m1["capabilities"])
	efforts := anySlice(m1["support_efforts"])
	if !slices.Equal(caps, []string{"thinking"}) || !slices.Equal(efforts, []string{"low", "high"}) {
		t.Fatalf("m1 reasoning projection = caps %v / efforts %v", caps, efforts)
	}
	m2 := models["senv-main/m2"].(map[string]any)
	if m2["max_context_size"] != int64(kimiMaxContextSizeFallback) {
		t.Fatalf("m2 max_context_size = %v, want fallback", m2["max_context_size"])
	}
	if _, ok := m2["capabilities"]; ok {
		t.Fatalf("m2 should not claim thinking without metadata: %v", m2)
	}
	m3 := models["senv-main/m3"].(map[string]any)
	if m3["display_name"] != "m3" || m3["provider"] != "senv-main" || m3["model"] != "m3" {
		t.Fatalf("m3 = %v", m3)
	}
}

func TestPiAndOpencodeProjectModelSets(t *testing.T) {
	home := t.TempDir()
	pi := piAdapter()
	applyProjection(t, pi, home, []string{"m1", "m2", "m3"}, "m2")
	piModels := readJSONFile(t, pi.ConfigPath(home))
	prov := piModels["providers"].(map[string]any)["senv-main"].(map[string]any)
	list := prov["models"].([]any)
	if len(list) != 3 {
		t.Fatalf("pi models = %#v, want 3 entries", list)
	}
	if list[0].(map[string]any)["id"] != "m1" || list[0].(map[string]any)["name"] != "Model One" {
		t.Fatalf("pi models[0] = %v", list[0])
	}
	m1 := list[0].(map[string]any)
	if m1["contextWindow"] != float64(300000) || m1["maxTokens"] != float64(32000) {
		t.Fatalf("pi m1 = %v, want contextWindow/maxTokens from metadata", m1)
	}
	if m1["reasoning"] != true {
		t.Fatalf("pi m1 reasoning = %v, want true with efforts", m1["reasoning"])
	}
	if _, ok := list[1].(map[string]any)["contextWindow"]; ok {
		t.Fatalf("pi m2 should omit unknown contextWindow: %v", list[1])
	}
	settings := readJSONFile(t, filepath.Join(filepath.Dir(pi.ConfigPath(home)), "settings.json"))
	if settings["defaultProvider"] != "senv-main" || settings["defaultModel"] != "m2" {
		t.Fatalf("pi settings = %v", settings)
	}

	opencode := opencodeAdapter()
	applyProjection(t, opencode, home, []string{"m1", "m2", "m3"}, "m2")
	oc := readJSONFile(t, opencode.ConfigPath(home))
	if oc["model"] != "senv-main/m2" {
		t.Fatalf("opencode model = %v", oc["model"])
	}
	ocProvider := oc["provider"].(map[string]any)["senv-main"].(map[string]any)
	if ocProvider["npm"] != "@ai-sdk/openai-compatible" {
		t.Fatalf("opencode npm = %v, want openai-compatible when shape undeclared", ocProvider["npm"])
	}
	ocModels := ocProvider["models"].(map[string]any)
	if len(ocModels) != 3 {
		t.Fatalf("opencode models = %v, want 3 entries", ocModels)
	}
	ocM1 := ocModels["m1"].(map[string]any)
	if ocM1["name"] != "Model One" {
		t.Fatalf("opencode models[m1] = %v", ocM1)
	}
	limit, ok := ocM1["limit"].(map[string]any)
	if !ok || limit["context"] != float64(300000) || limit["output"] != float64(32000) {
		t.Fatalf("opencode m1 limit = %v, want context/output from metadata", ocM1["limit"])
	}
	if ocM1["reasoning"] != true {
		t.Fatalf("opencode m1 reasoning = %v, want true with efforts", ocM1["reasoning"])
	}
	if _, ok := ocModels["m2"].(map[string]any)["limit"]; ok {
		t.Fatalf("opencode m2 should omit unknown limit: %v", ocModels["m2"])
	}
}

// TestAdapterDeclaredShapePicksWireProtocol 覆盖 ADR-0006 的 OpenAI 兼容族内
// 细分：显式声明的 api_shape 决定各 agent 的线协议字段，未声明时保持原默认。
func TestAdapterDeclaredShapePicksWireProtocol(t *testing.T) {
	home := t.TempDir()

	pi := piAdapter()
	req := projectionRequest(t, pi, home, []string{"m1"}, "m1")
	req.APIShape = "openai-responses"
	if err := pi.Apply(req); err != nil {
		t.Fatalf("pi Apply() error = %v", err)
	}
	piProv := readJSONFile(t, pi.ConfigPath(home))["providers"].(map[string]any)["senv-main"].(map[string]any)
	if piProv["api"] != "openai-responses" {
		t.Fatalf("pi api = %v, want openai-responses for declared shape", piProv["api"])
	}

	kimi := kimiAdapter()
	kreq := projectionRequest(t, kimi, home, []string{"m1"}, "m1")
	kreq.APIShape = "openai-responses"
	if err := kimi.Apply(kreq); err != nil {
		t.Fatalf("kimi Apply() error = %v", err)
	}
	kimiCfg := readTOMLFile(t, kimi.ConfigPath(home))
	kimiProv := kimiCfg["providers"].(map[string]any)["senv-main"].(map[string]any)
	if kimiProv["type"] != "openai_responses" {
		t.Fatalf("kimi type = %v, want openai_responses for declared shape", kimiProv["type"])
	}

	codex := codexAdapter()
	creq := projectionRequest(t, codex, home, []string{"m1"}, "m1")
	creq.APIShape = "openai-chat"
	if err := codex.Apply(creq); err != nil {
		t.Fatalf("codex Apply() error = %v", err)
	}
	codexCfg := readTOMLFile(t, codex.ConfigPath(home))
	codexProv := codexCfg["model_providers"].(map[string]any)["senv-main"].(map[string]any)
	if codexProv["wire_api"] != "chat" {
		t.Fatalf("codex wire_api = %v, want chat for declared openai-chat", codexProv["wire_api"])
	}

	opencode := opencodeAdapter()
	oreq := projectionRequest(t, opencode, home, []string{"m1"}, "m1")
	oreq.APIShape = "openai-responses"
	if err := opencode.Apply(oreq); err != nil {
		t.Fatalf("opencode Apply() error = %v", err)
	}
	ocNPM := readJSONFile(t, opencode.ConfigPath(home))["provider"].(map[string]any)["senv-main"].(map[string]any)["npm"]
	if ocNPM != "@ai-sdk/openai" {
		t.Fatalf("opencode npm = %v, want @ai-sdk/openai for declared responses", ocNPM)
	}
}

// anySlice 把 TOML/JSON 解析出的数组统一成字符串切片（非字符串元素丢弃）。
func anySlice(raw any) []string {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

// projectionRequest 构造一个未落盘的 SwitchRequest（只用于直接调用 Apply）。
func projectionRequest(t *testing.T, a AgentAdapter, home string, models []string, defaultModel string) SwitchRequest {
	t.Helper()
	req := SwitchRequest{
		AgentID:       a.ID,
		ProviderAlias: "main",
		BaseURL:       "https://api.example.com",
		Models:        models,
		DefaultModel:  defaultModel,
		Credential:    "sk-secret",
		ConfigPath:    a.ConfigPath(home),
		Home:          home,
		ModelMetadata: projectionMeta(),
	}
	if a.Credential == CredentialEnvVar {
		req.Credential = senvEnvKeyName("main")
	}
	return req
}

// TestAdapterProjectionIsIdempotent 覆盖任务 3.5：五个 agent 重复切换同一模型
// 集与默认模型时产物逐字节不变。
func TestAdapterProjectionIsIdempotent(t *testing.T) {
	for _, a := range SupportedAgents() {
		t.Run(a.ID, func(t *testing.T) {
			home := t.TempDir()
			models := []string{"m1", "m2", "m3"}
			applyProjection(t, a, home, models, "m2")
			first := map[string][]byte{}
			for _, path := range projectionPaths(a, home) {
				first[path] = mustRead(t, path)
			}
			applyProjection(t, a, home, models, "m2")
			for path, before := range first {
				if got := mustRead(t, path); string(got) != string(before) {
					t.Fatalf("%s changed on repeat switch:\nbefore:\n%s\nafter:\n%s", path, before, got)
				}
			}
		})
	}
}

// projectionPaths 返回适配器在 home 下会写入的全部文件（配置文件 + 派生文件）。
func projectionPaths(a AgentAdapter, home string) []string {
	paths := a.ConfigPaths(home)
	if a.OwnedArtifacts != nil {
		paths = append(paths, a.OwnedArtifacts(home, "main")...)
	}
	return paths
}

// TestCodexCatalogParsedByCodexBinary 是本机回环验证：生成的 catalog 必须能被
// codex 自己解析。codex 不在 PATH 时跳过。
func TestCodexCatalogParsedByCodexBinary(t *testing.T) {
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex binary not available")
	}
	home := t.TempDir()
	applyProjection(t, codexAdapter(), home, []string{"m1", "m2", "m3"}, "m2")

	cmd := exec.Command(codexPath, "debug", "models")
	cmd.Env = codexTestEnv(home)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		stderr := ""
		if errors.As(err, &exitErr) {
			stderr = string(exitErr.Stderr)
		}
		t.Fatalf("codex debug models failed: %v\nstderr: %s", err, stderr)
	}
	var parsed struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("parse codex output: %v", err)
	}
	var slugs []string
	for _, model := range parsed.Models {
		slugs = append(slugs, model.Slug)
	}
	if !slices.Equal(slugs, []string{"m1", "m2", "m3"}) {
		t.Fatalf("codex catalog slugs = %v, want the Agent model set", slugs)
	}
}

func codexTestEnv(home string) []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "HOME=") || strings.HasPrefix(kv, "CODEX_HOME=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "HOME="+home, "CODEX_HOME="+filepath.Join(home, ".codex"))
}

func addProjectionProvider(t *testing.T, pm *ProviderManager, alias string, models []string, defaultModel string) {
	t.Helper()
	if _, err := pm.AddProvider(AddProviderOptions{
		Alias:        alias,
		BaseURL:      "https://api.example.com",
		APIKey:       "sk-" + alias,
		Models:       models,
		DefaultModel: defaultModel,
	}); err != nil {
		t.Fatalf("AddProvider(%s) error = %v", alias, err)
	}
}

// TestSwitchShrinksModelSetAndKeepsUserEntries 覆盖任务 4.1/4.2：缩集时清掉
// 上次写入、本次未选中的条目，用户自有条目原位保留。
func TestSwitchShrinksModelSetAndKeepsUserEntries(t *testing.T) {
	pm, _, _ := newTestProviderManager(t)
	addProjectionProvider(t, pm, "main", []string{"m1", "m2", "m3"}, "m2")
	home := t.TempDir()
	configPath := filepath.Join(home, ".kimi-code", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	seed := "[providers.user]\nbase_url = \"https://user.example\"\n\n[models.\"user/custom\"]\nprovider = \"user\"\n"
	if err := os.WriteFile(configPath, []byte(seed), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	sm := NewSwitchManager(pm, "", home)
	if _, err := sm.Switch("kimi", "main", []string{"m1", "m2"}, "m2"); err != nil {
		t.Fatalf("first Switch() error = %v", err)
	}
	if _, err := sm.Switch("kimi", "main", []string{"m2"}, "m2"); err != nil {
		t.Fatalf("second Switch() error = %v", err)
	}

	cfg := readTOMLFile(t, configPath)
	models := cfg["models"].(map[string]any)
	if _, ok := models["senv-main/m1"]; ok {
		t.Fatal("stale model entry senv-main/m1 kept after shrink")
	}
	if _, ok := models["senv-main/m2"]; !ok {
		t.Fatal("selected model entry senv-main/m2 missing")
	}
	if _, ok := models["user/custom"]; !ok {
		t.Fatal("user-owned model entry was removed")
	}
	providers := cfg["providers"].(map[string]any)
	if _, ok := providers["senv-main"]; !ok {
		t.Fatal("provider entry missing after shrink")
	}
	if _, ok := providers["user"]; !ok {
		t.Fatal("user-owned provider entry was removed")
	}
	if cfg["default_model"] != "senv-main/m2" {
		t.Fatalf("default_model = %v", cfg["default_model"])
	}
}

// TestSwitchProviderChangeCleansOldNamespace 覆盖任务 4.1：换 provider 后旧
// 命名空间的 provider 与模型条目都被删除。
func TestSwitchProviderChangeCleansOldNamespace(t *testing.T) {
	pm, _, _ := newTestProviderManager(t)
	addProjectionProvider(t, pm, "main", []string{"m1", "m2"}, "m1")
	addProjectionProvider(t, pm, "alt", []string{"x1", "x2"}, "x1")
	home := t.TempDir()

	sm := NewSwitchManager(pm, "", home)
	if _, err := sm.Switch("kimi", "main", nil, ""); err != nil {
		t.Fatalf("Switch(main) error = %v", err)
	}
	if _, err := sm.Switch("kimi", "alt", nil, ""); err != nil {
		t.Fatalf("Switch(alt) error = %v", err)
	}

	cfg := readTOMLFile(t, filepath.Join(home, ".kimi-code", "config.toml"))
	models := cfg["models"].(map[string]any)
	for key := range models {
		if strings.HasPrefix(key, "senv-main/") {
			t.Fatalf("old provider model entry %q kept after provider change", key)
		}
	}
	if _, ok := models["senv-alt/x1"]; !ok {
		t.Fatalf("new provider model entries missing: %v", models)
	}
	providers := cfg["providers"].(map[string]any)
	if _, ok := providers["senv-main"]; ok {
		t.Fatal("old provider entry kept after provider change")
	}
	if cfg["default_model"] != "senv-alt/x1" {
		t.Fatalf("default_model = %v", cfg["default_model"])
	}
}

// TestSwitchRemovesStaleCodexCatalog 覆盖任务 4.1 的失效派生文件清理：codex
// 换 provider 后不再被指向的 catalog 文件被删除。
func TestSwitchRemovesStaleCodexCatalog(t *testing.T) {
	pm, _, _ := newTestProviderManager(t)
	addProjectionProvider(t, pm, "main", []string{"m1", "m2"}, "m1")
	addProjectionProvider(t, pm, "alt", []string{"x1"}, "x1")
	home := t.TempDir()

	sm := NewSwitchManager(pm, "", home)
	if _, err := sm.Switch("codex", "main", nil, ""); err != nil {
		t.Fatalf("Switch(main) error = %v", err)
	}
	if _, err := os.Stat(codexCatalogPath(home, "main")); err != nil {
		t.Fatalf("catalog for main missing: %v", err)
	}
	if _, err := sm.Switch("codex", "alt", nil, ""); err != nil {
		t.Fatalf("Switch(alt) error = %v", err)
	}
	if _, err := os.Stat(codexCatalogPath(home, "main")); !os.IsNotExist(err) {
		t.Fatalf("stale catalog for main kept: err = %v", err)
	}
	if _, err := os.Stat(codexCatalogPath(home, "alt")); err != nil {
		t.Fatalf("catalog for alt missing: %v", err)
	}
	cfg := readTOMLFile(t, filepath.Join(home, ".codex", "config.toml"))
	if cfg["model_catalog_json"] != codexCatalogRelPath("alt") {
		t.Fatalf("model_catalog_json = %v", cfg["model_catalog_json"])
	}
}

// TestStaleCodexCatalogKeptWhileOtherAgentPointsAtProvider 覆盖任务 4.2 的另一
// 半：alias 仍被别的 agent 指针引用时，catalog 文件保留。
func TestStaleCodexCatalogKeptWhileOtherAgentPointsAtProvider(t *testing.T) {
	pm, _, _ := newTestProviderManager(t)
	addProjectionProvider(t, pm, "main", []string{"m1"}, "m1")
	addProjectionProvider(t, pm, "alt", []string{"x1"}, "x1")
	home := t.TempDir()

	sm := NewSwitchManager(pm, "", home)
	if _, err := sm.Switch("codex", "main", nil, ""); err != nil {
		t.Fatalf("Switch(codex, main) error = %v", err)
	}
	if _, err := sm.Switch("claude-code", "main", nil, ""); err != nil {
		t.Fatalf("Switch(claude-code, main) error = %v", err)
	}
	if _, err := sm.Switch("codex", "alt", nil, ""); err != nil {
		t.Fatalf("Switch(codex, alt) error = %v", err)
	}
	if _, err := os.Stat(codexCatalogPath(home, "main")); err != nil {
		t.Fatalf("catalog still referenced by claude-code was removed: %v", err)
	}
}

// TestSwitchRollsBackNewCatalogOnConfigFailure 覆盖任务 5.1：catalog 新建成功
// 但后续配置写回失败时，整个事务回滚（新建文件删除、配置与指针不变）。
func TestSwitchRollsBackNewCatalogOnConfigFailure(t *testing.T) {
	pm, _, _ := newTestProviderManager(t)
	addProjectionProvider(t, pm, "main", []string{"m1"}, "m1")
	home := t.TempDir()
	configDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	broken := "= = = not toml\n"
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(broken), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	sm := NewSwitchManager(pm, "", home)
	if _, err := sm.Switch("codex", "main", nil, ""); err == nil {
		t.Fatal("Switch() unexpectedly succeeded on broken config")
	}
	if got := string(mustRead(t, configPath)); got != broken {
		t.Fatalf("config changed after rollback: %q", got)
	}
	if _, err := os.Stat(codexCatalogPath(home, "main")); !os.IsNotExist(err) {
		t.Fatalf("newly written catalog kept after rollback: err = %v", err)
	}
	if _, err := os.Stat(DefaultPointerPath(home)); !os.IsNotExist(err) {
		t.Fatalf("pointer file written despite failure: err = %v", err)
	}
}

// TestSwitchToleratesMissingStaleArtifacts 覆盖任务 4.2：清理目标本就不存在时
// 静默通过，重复/手工清理过的机器仍能正常切换。
func TestSwitchToleratesMissingStaleArtifacts(t *testing.T) {
	pm, _, _ := newTestProviderManager(t)
	addProjectionProvider(t, pm, "main", []string{"m1"}, "m1")
	addProjectionProvider(t, pm, "alt", []string{"x1"}, "x1")
	home := t.TempDir()

	sm := NewSwitchManager(pm, "", home)
	if _, err := sm.Switch("codex", "main", nil, ""); err != nil {
		t.Fatalf("Switch(main) error = %v", err)
	}
	if err := os.Remove(codexCatalogPath(home, "main")); err != nil {
		t.Fatalf("Remove(catalog) error = %v", err)
	}
	if _, err := sm.Switch("codex", "alt", nil, ""); err != nil {
		t.Fatalf("Switch(alt) with missing stale artifact error = %v", err)
	}
}

func TestCodexUsesDeclaredDefaultNotFirstEffort(t *testing.T) {
	home := t.TempDir()
	a := codexAdapter()
	req := projectionRequest(t, a, home, []string{"m1"}, "m1")
	req.ModelMetadata = map[string]ModelMetadata{
		"m1": {ReasoningEfforts: []string{"low", "high"}, DefaultReasoning: "high", InputModalities: []string{"text", "image"}},
	}
	if err := a.Apply(req); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	var catalog struct {
		Models []struct {
			DefaultReasoningLevel    string   `json:"default_reasoning_level"`
			InputModalities          []string `json:"input_modalities"`
			SupportedReasoningLevels []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if err := json.Unmarshal(mustRead(t, codexCatalogPath(home, "main")), &catalog); err != nil {
		t.Fatalf("parse catalog: %v", err)
	}
	got := catalog.Models[0]
	if got.DefaultReasoningLevel != "high" {
		t.Fatalf("default = %q, want declared high", got.DefaultReasoningLevel)
	}
	if len(got.SupportedReasoningLevels) != 2 || got.SupportedReasoningLevels[0].Effort != "low" {
		t.Fatalf("levels = %+v", got.SupportedReasoningLevels)
	}
	if !slices.Equal(got.InputModalities, []string{"text", "image"}) {
		t.Fatalf("input_modalities = %v", got.InputModalities)
	}
}

func TestCodexMissingDefaultUsesNoneTemplate(t *testing.T) {
	home := t.TempDir()
	a := codexAdapter()
	req := projectionRequest(t, a, home, []string{"m1"}, "m1")
	req.ModelMetadata = map[string]ModelMetadata{
		"m1": {ReasoningEfforts: []string{"low", "high"}},
	}
	if err := a.Apply(req); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	var catalog struct {
		Models []struct {
			DefaultReasoningLevel    string   `json:"default_reasoning_level"`
			InputModalities          []string `json:"input_modalities"`
			SupportedReasoningLevels []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if err := json.Unmarshal(mustRead(t, codexCatalogPath(home, "main")), &catalog); err != nil {
		t.Fatalf("parse catalog: %v", err)
	}
	got := catalog.Models[0]
	if got.DefaultReasoningLevel != "none" || len(got.SupportedReasoningLevels) != 1 || got.SupportedReasoningLevels[0].Effort != "none" {
		t.Fatalf("missing default should use none template: %+v", got)
	}
	if !slices.Equal(got.InputModalities, []string{"text"}) {
		t.Fatalf("missing modalities = %v, want text template", got.InputModalities)
	}
}

func TestProjectionInputModalities(t *testing.T) {
	home := t.TempDir()
	meta := map[string]ModelMetadata{
		"m1": {Name: "M1", ContextLimit: 1000, ReasoningEfforts: []string{"high"}, DefaultReasoning: "high", InputModalities: []string{"text", "image", "video"}},
	}

	kimi := kimiAdapter()
	req := projectionRequest(t, kimi, home, []string{"m1"}, "m1")
	req.ModelMetadata = meta
	if err := kimi.Apply(req); err != nil {
		t.Fatalf("kimi Apply() error = %v", err)
	}
	kimiM1 := readTOMLFile(t, kimi.ConfigPath(home))["models"].(map[string]any)["senv-main/m1"].(map[string]any)
	caps := anySlice(kimiM1["capabilities"])
	if !slices.Equal(caps, []string{"thinking", "image_in", "video_in"}) {
		t.Fatalf("kimi capabilities = %v", caps)
	}

	pi := piAdapter()
	preq := projectionRequest(t, pi, home, []string{"m1"}, "m1")
	preq.ModelMetadata = meta
	if err := pi.Apply(preq); err != nil {
		t.Fatalf("pi Apply() error = %v", err)
	}
	piM1 := readJSONFile(t, pi.ConfigPath(home))["providers"].(map[string]any)["senv-main"].(map[string]any)["models"].([]any)[0].(map[string]any)
	// pi schema 仅允许 text/image，video 被过滤而不是透传。
	if got := anySlice(piM1["input"]); !slices.Equal(got, []string{"text", "image"}) {
		t.Fatalf("pi input = %v", piM1["input"])
	}

	opencode := opencodeAdapter()
	oreq := projectionRequest(t, opencode, home, []string{"m1"}, "m1")
	oreq.ModelMetadata = meta
	if err := opencode.Apply(oreq); err != nil {
		t.Fatalf("opencode Apply() error = %v", err)
	}
	ocM1 := readJSONFile(t, opencode.ConfigPath(home))["provider"].(map[string]any)["senv-main"].(map[string]any)["models"].(map[string]any)["m1"].(map[string]any)
	mods := ocM1["modalities"].(map[string]any)
	if got := anySlice(mods["input"]); !slices.Equal(got, []string{"text", "image", "video"}) {
		t.Fatalf("opencode modalities.input = %v", mods["input"])
	}
}

// TestProjectionFiltersUnsupportedModalities 覆盖 models.dev 声明超出 agent
// schema 取值范围的模态（audio/pdf/video）时的收敛行为：pi 白名单 text/image、
// codex 白名单 text/image/audio，过滤后为空各自回退省略/["text"]，opencode
// 与 kimi 原样保留。
func TestProjectionFiltersUnsupportedModalities(t *testing.T) {
	home := t.TempDir()
	meta := map[string]ModelMetadata{
		// MiniMax-M3 真实目录形态：text/image/video。
		"vision": {Name: "Vision", InputModalities: []string{"text", "image", "video"}},
		// 纯视频模型：过滤后为空。
		"video-only": {Name: "VideoOnly", InputModalities: []string{"video"}},
		// Gemini 形态：audio/pdf 也不在 pi/codex 白名单内。
		"omni": {Name: "Omni", InputModalities: []string{"text", "audio", "pdf"}},
	}
	models := []string{"vision", "video-only", "omni"}

	pi := piAdapter()
	preq := projectionRequest(t, pi, home, models, "vision")
	preq.ModelMetadata = meta
	if err := pi.Apply(preq); err != nil {
		t.Fatalf("pi Apply() error = %v", err)
	}
	entries := readJSONFile(t, pi.ConfigPath(home))["providers"].(map[string]any)["senv-main"].(map[string]any)["models"].([]any)
	wantPi := [][]string{{"text", "image"}, nil, {"text"}}
	for i, want := range wantPi {
		entry := entries[i].(map[string]any)
		got, ok := entry["input"]
		if want == nil {
			if ok {
				t.Fatalf("pi models[%d] input = %v, want field omitted", i, got)
			}
			continue
		}
		if !ok || !slices.Equal(anySlice(got), want) {
			t.Fatalf("pi models[%d] input = %v, want %v", i, got, want)
		}
	}

	codex := codexAdapter()
	creq := projectionRequest(t, codex, home, models, "vision")
	creq.ModelMetadata = meta
	if err := codex.Apply(creq); err != nil {
		t.Fatalf("codex Apply() error = %v", err)
	}
	var catalog struct {
		Models []struct {
			Slug            string   `json:"slug"`
			InputModalities []string `json:"input_modalities"`
		} `json:"models"`
	}
	data, err := os.ReadFile(filepath.Join(home, ".codex", "model-catalogs", "senv-main.json"))
	if err != nil {
		t.Fatalf("read codex catalog: %v", err)
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatalf("parse codex catalog: %v", err)
	}
	wantCodex := map[string][]string{
		"vision":     {"text", "image"},
		"video-only": {"text"},
		"omni":       {"text", "audio"},
	}
	for _, entry := range catalog.Models {
		if !slices.Equal(entry.InputModalities, wantCodex[entry.Slug]) {
			t.Fatalf("codex %s input_modalities = %v, want %v", entry.Slug, entry.InputModalities, wantCodex[entry.Slug])
		}
	}

	opencode := opencodeAdapter()
	oreq := projectionRequest(t, opencode, home, models, "vision")
	oreq.ModelMetadata = meta
	if err := opencode.Apply(oreq); err != nil {
		t.Fatalf("opencode Apply() error = %v", err)
	}
	ocModels := readJSONFile(t, opencode.ConfigPath(home))["provider"].(map[string]any)["senv-main"].(map[string]any)["models"].(map[string]any)
	wantOC := map[string][]string{
		"vision":     {"text", "image", "video"},
		"video-only": {"video"},
		"omni":       {"text", "audio", "pdf"},
	}
	for slug, want := range wantOC {
		mods := ocModels[slug].(map[string]any)["modalities"].(map[string]any)
		if got := anySlice(mods["input"]); !slices.Equal(got, want) {
			t.Fatalf("opencode %s modalities.input = %v, want %v", slug, got, want)
		}
	}
}
