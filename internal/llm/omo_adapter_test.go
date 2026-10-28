package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFileAt creates path's parent directories and writes content.
func writeFileAt(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func readJSONAt(t *testing.T, path string) map[string]any {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(mustRead(t, path), &root); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return root
}

// clearOmOEnv makes OmoAgentDir resolve to the default ~/.omo/agent branch.
func clearOmOEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"OMO_CODING_AGENT_DIR", "SENPI_CODING_AGENT_DIR", "PI_CODING_AGENT_DIR"} {
		t.Setenv(key, "")
	}
}

func TestOmOAdapterWritesProviderAndDefaults(t *testing.T) {
	clearOmOEnv(t)
	home := t.TempDir()
	a := omoAdapter()
	if a.ID != "omo" || a.Name != "OmO" {
		t.Fatalf("adapter identity = %q/%q", a.ID, a.Name)
	}
	applyProjection(t, a, home, []string{"m1", "m2", "m3"}, "m2")

	agentDir := filepath.Join(home, ".omo", "agent")
	if got, want := a.ConfigPath(home), filepath.Join(agentDir, "models.json"); got != want {
		t.Fatalf("ConfigPath = %q, want %q", got, want)
	}
	models := readJSONAt(t, a.ConfigPath(home))
	prov, ok := models["providers"].(map[string]any)["senv-main"].(map[string]any)
	if !ok {
		t.Fatalf("models.json = %v, want providers.senv-main", models)
	}
	if prov["baseUrl"] != "https://api.example.com" {
		t.Fatalf("baseUrl = %v", prov["baseUrl"])
	}
	if prov["api"] != "openai-completions" {
		t.Fatalf("api = %v, want openai-completions with no declared shape", prov["api"])
	}
	compat, ok := prov["compat"].(map[string]any)
	if !ok || compat["supportsDeveloperRole"] != false {
		t.Fatalf("compat = %v, want supportsDeveloperRole:false at provider level", prov["compat"])
	}
	if _, has := compat["supportsReasoningEffort"]; has {
		t.Fatalf("compat must not disable reasoning_effort passthrough: %v", compat)
	}
	list, ok := prov["models"].([]any)
	if !ok || len(list) != 3 {
		t.Fatalf("models = %v, want 3 entries", prov["models"])
	}
	m1 := list[0].(map[string]any)
	if m1["id"] != "m1" || m1["contextWindow"] != float64(300000) || m1["maxTokens"] != float64(32000) {
		t.Fatalf("m1 = %v, want id plus projected contextWindow/maxTokens", m1)
	}
	if m1["reasoning"] != true {
		t.Fatalf("m1 reasoning = %v, want true", m1["reasoning"])
	}

	settings := readJSONAt(t, filepath.Join(agentDir, "settings.json"))
	if settings["defaultProvider"] != "senv-main" || settings["defaultModel"] != "m2" {
		t.Fatalf("settings = %v, want defaultProvider/defaultModel", settings)
	}
	// No enabledModels existed, so none may be invented.
	if _, has := settings["enabledModels"]; has {
		t.Fatalf("settings must not create enabledModels when absent: %v", settings)
	}
}

func TestOmOAdapterOrdersDefaultModelInEnabledScope(t *testing.T) {
	clearOmOEnv(t)
	home := t.TempDir()
	a := omoAdapter()
	settingsPath := filepath.Join(home, ".omo", "agent", "settings.json")
	writeFileAt(t, settingsPath, `{
		"theme": "dark",
		"defaultProvider": "senv-old",
		"defaultModel": "old-model",
		"enabledModels": ["minimax-cn/*", "senv-old/*", "senv-main/stale", "custom/*"]
	}`)
	req := SwitchRequest{
		AgentID: a.ID, ProviderAlias: "main", BaseURL: "https://api.example.com",
		Models: []string{"m1", "m2"}, DefaultModel: "m2", Credential: "sk-secret",
		ConfigPath: a.ConfigPath(home), Home: home, PriorProvider: "old",
	}
	if err := a.Apply(req); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	settings := readJSONAt(t, settingsPath)
	if settings["defaultProvider"] != "senv-main" || settings["defaultModel"] != "m2" || settings["theme"] != "dark" {
		t.Fatalf("settings = %v", settings)
	}
	raw := settings["enabledModels"].([]any)
	got := make([]string, 0, len(raw))
	for _, item := range raw {
		got = append(got, item.(string))
	}
	want := []string{"senv-main/m2", "senv-main/*", "minimax-cn/*", "custom/*"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("enabledModels = %v, want %v", got, want)
	}
}

func TestOmOAdapterClearsPriorProvider(t *testing.T) {
	clearOmOEnv(t)
	home := t.TempDir()
	a := omoAdapter()
	modelsPath := a.ConfigPath(home)
	writeFileAt(t, modelsPath, `{
		"providers": {
			"senv-old": {"baseUrl": "https://old.example.com", "api": "openai-completions"},
			"user-own": {"baseUrl": "https://mine.example.com", "api": "openai-completions"}
		}
	}`)
	req := SwitchRequest{
		AgentID: a.ID, ProviderAlias: "main", BaseURL: "https://api.example.com",
		Models: []string{"m1"}, DefaultModel: "m1", Credential: "sk-secret",
		ConfigPath: modelsPath, Home: home, PriorProvider: "old",
	}
	if err := a.Apply(req); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	providers := readJSONAt(t, modelsPath)["providers"].(map[string]any)
	if _, has := providers["senv-old"]; has {
		t.Fatalf("prior senv provider must be removed: %v", providers)
	}
	if _, has := providers["user-own"]; !has {
		t.Fatalf("user-owned provider must be preserved: %v", providers)
	}
	if _, has := providers["senv-main"]; !has {
		t.Fatalf("new senv provider missing: %v", providers)
	}
}

func TestOmOAdapterHonorsEnvAgentDir(t *testing.T) {
	clearOmOEnv(t)
	alt := filepath.Join(t.TempDir(), "alt")
	t.Setenv("OMO_CODING_AGENT_DIR", alt)
	a := omoAdapter()
	if got, want := a.ConfigPath("/home/u"), filepath.Join(alt, "models.json"); got != want {
		t.Fatalf("ConfigPath = %q, want %q", got, want)
	}
	if paths := a.ConfigPaths("/home/u"); len(paths) != 2 || paths[0] != filepath.Join(alt, "models.json") || paths[1] != filepath.Join(alt, "settings.json") {
		t.Fatalf("ConfigPaths = %v", paths)
	}
}

func TestOmOAdapterProjectsDeclaredResponsesShape(t *testing.T) {
	clearOmOEnv(t)
	home := t.TempDir()
	a := omoAdapter()
	req := SwitchRequest{
		AgentID: a.ID, ProviderAlias: "main", BaseURL: "https://api.example.com",
		Models: []string{"m1"}, DefaultModel: "m1", Credential: "sk-secret",
		ConfigPath: a.ConfigPath(home), Home: home, APIShape: "openai-responses",
	}
	if err := a.Apply(req); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	prov := readJSONAt(t, a.ConfigPath(home))["providers"].(map[string]any)["senv-main"].(map[string]any)
	if prov["api"] != "openai-responses" {
		t.Fatalf("api = %v, want openai-responses from declared shape", prov["api"])
	}
}

func TestOmOAdapterCredentialEnvRef(t *testing.T) {
	clearOmOEnv(t)
	a := omoAdapter()
	if a.CredentialEnvRef == nil {
		t.Fatal("omo must support env-var credential refs (ADR-0031)")
	}
	if got := a.CredentialEnvRef("KIMI_API_KEY"); got != "$KIMI_API_KEY" {
		t.Fatalf("CredentialEnvRef = %q, want $KIMI_API_KEY", got)
	}
}

func TestLookupAgentAcceptsOmOAliases(t *testing.T) {
	for _, id := range []string{"omo", "OMO", "oh-my-openagent", "Oh-My-OpenAgent"} {
		a, ok := LookupAgent(id)
		if !ok || a.ID != "omo" {
			t.Fatalf("LookupAgent(%q) = %q, %v; want omo", id, a.ID, ok)
		}
	}
	for _, a := range SupportedAgents() {
		if a.ID == "oh-my-openagent" {
			t.Fatal("SupportedAgents must list canonical ids only")
		}
	}
}
