package llm

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/wii/senv/internal/storage"
)

// ADR-0029：后台模型的解析链是「单次切换指定 > 档案声明 > 默认模型」，
// 显式值与档案声明都必须属于 Provider 模型集（fail-closed）。

func TestClaudeCodeAdapterWritesBackgroundModel(t *testing.T) {
	home := t.TempDir()
	a := claudeCodeAdapter()
	req := SwitchRequest{
		AgentID: a.ID, ProviderAlias: "main", BaseURL: "https://api.example.com",
		Models: []string{"m1", "m2"}, DefaultModel: "m1", Credential: "sk-secret",
		ConfigPath: a.ConfigPath(home), Home: home,
		BackgroundModel: "m2",
	}
	if err := a.Apply(req); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(mustRead(t, req.ConfigPath), &root); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	env := root["env"].(map[string]any)
	if got := env["ANTHROPIC_SMALL_FAST_MODEL"]; got != "m2" {
		t.Fatalf("ANTHROPIC_SMALL_FAST_MODEL = %v, want m2", got)
	}
	if got := env["ANTHROPIC_DEFAULT_HAIKU_MODEL"]; got != "m2" {
		t.Fatalf("ANTHROPIC_DEFAULT_HAIKU_MODEL = %v, want m2", got)
	}
}

func TestResolveBackgroundModel(t *testing.T) {
	entry := func(declared string) *storage.LLMProviderEntry {
		return &storage.LLMProviderEntry{
			Alias: "main", Models: []string{"m1", "m2"}, BackgroundModel: declared,
		}
	}

	// 显式指定优先于档案声明。
	if got, fallback, err := resolveBackgroundModel("main", entry("m2"), "m1", "m2"); err != nil || fallback || got != "m1" {
		t.Fatalf("explicit = %q, %v, %v; want m1, false, nil", got, fallback, err)
	}
	// 档案声明次之。
	if got, fallback, err := resolveBackgroundModel("main", entry("m2"), "", "m1"); err != nil || fallback || got != "m2" {
		t.Fatalf("declared = %q, %v, %v; want m2, false, nil", got, fallback, err)
	}
	// 都缺省时回退默认模型并标记 fallback。
	if got, fallback, err := resolveBackgroundModel("main", entry(""), " ", "m1"); err != nil || !fallback || got != "m1" {
		t.Fatalf("fallback = %q, %v, %v; want m1, true, nil", got, fallback, err)
	}
	// 显式值不在模型集：fail-closed。
	if _, _, err := resolveBackgroundModel("main", entry(""), "ghost", "m1"); err == nil ||
		!strings.Contains(err.Error(), "not in provider") {
		t.Fatalf("explicit ghost err = %v, want membership error", err)
	}
	// 档案声明不在模型集（旧版本写入的档案）：fail-closed 并指向 edit 修复。
	if _, _, err := resolveBackgroundModel("main", entry("ghost"), "", "m1"); err == nil ||
		!strings.Contains(err.Error(), "provider edit") {
		t.Fatalf("declared ghost err = %v, want edit guidance", err)
	}
}

func TestSwitchBackgroundModelChain(t *testing.T) {
	newClaudeSwitch := func(t *testing.T, declared string) (*SwitchManager, string) {
		pm, _, _ := newTestProviderManager(t)
		if _, err := pm.AddProvider(AddProviderOptions{
			Alias: "main", BaseURL: "https://api.example.com", APIKey: "sk-secret",
			Models: []string{"m1", "m2"}, ModelContexts: map[string]int{"m1": 128000, "m2": 128000},
			DefaultModel: "m1", BackgroundModel: declared,
		}); err != nil {
			t.Fatalf("AddProvider() error = %v", err)
		}
		home := t.TempDir()
		return NewSwitchManager(pm, "", home), home
	}
	readEnv := func(t *testing.T, home string) map[string]any {
		var root map[string]any
		if err := json.Unmarshal(mustRead(t, home+"/.claude/settings.json"), &root); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		return root["env"].(map[string]any)
	}

	// 档案声明直达配置（SMALL_FAST 与 DEFAULT_HAIKU 同值）。
	sm, home := newClaudeSwitch(t, "m2")
	out, err := sm.Switch("claude-code", "main", nil, "", "")
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if env := readEnv(t, home); out.BackgroundModel != "m2" ||
		env["ANTHROPIC_SMALL_FAST_MODEL"] != "m2" || env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "m2" {
		t.Fatalf("declared: out = %q, env = %v; want m2", out.BackgroundModel, env)
	}
	if anyWarningContains(out.Warnings, "后台模型") {
		t.Fatalf("declared switch must not warn about fallback: %v", out.Warnings)
	}

	// 单次切换覆盖档案声明，不回写档案。
	sm, home = newClaudeSwitch(t, "m2")
	out, err = sm.Switch("claude-code", "main", nil, "", "m1")
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if out.BackgroundModel != "m1" {
		t.Fatalf("override: out = %q, want m1", out.BackgroundModel)
	}
	entry, err := sm.providerManager.GetProvider("main")
	if err != nil || entry.BackgroundModel != "m2" {
		t.Fatalf("profile must keep the declared m2, got %q (%v)", entry.BackgroundModel, err)
	}

	// 未声明时回退默认模型并给 warning。
	sm, home = newClaudeSwitch(t, "")
	out, err = sm.Switch("claude-code", "main", nil, "", "")
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if out.BackgroundModel != "m1" {
		t.Fatalf("fallback: out = %q, want m1", out.BackgroundModel)
	}
	if env := readEnv(t, home); env["ANTHROPIC_SMALL_FAST_MODEL"] != "m1" ||
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "m1" {
		t.Fatalf("fallback: env = %v, want m1", env)
	}
	if !anyWarningContains(out.Warnings, "provider edit") {
		t.Fatalf("fallback must warn with edit guidance: %v", out.Warnings)
	}

	// 显式值不在模型集：拒绝且零写入。
	sm, home = newClaudeSwitch(t, "")
	if _, err = sm.Switch("claude-code", "main", nil, "", "ghost"); err == nil ||
		!strings.Contains(err.Error(), "not in provider") {
		t.Fatalf("ghost override err = %v, want membership error", err)
	}
	if _, err := os.Stat(home + "/.claude/settings.json"); !os.IsNotExist(err) {
		t.Fatal("rejected switch must not write the config")
	}
}

func TestSwitchBackgroundModelIgnoredForNonConsumer(t *testing.T) {
	pm, _, _ := newTestProviderManager(t)
	if _, err := pm.AddProvider(AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models: []string{"m1"}, ModelContexts: map[string]int{"m1": 128000},
	}); err != nil {
		t.Fatalf("AddProvider() error = %v", err)
	}
	home := t.TempDir()
	// codex 不消费后台模型：显式值既不校验也不落盘（TUI 批量切换会把同一份
	// 值传给所有选中 agent，非消费方必须静默忽略）。
	out, err := NewSwitchManager(pm, "", home).Switch("codex", "main", nil, "", "ghost")
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if out.BackgroundModel != "" {
		t.Fatalf("codex output BackgroundModel = %q, want empty", out.BackgroundModel)
	}
	raw := string(mustRead(t, home+"/.codex/config.toml"))
	if strings.Contains(raw, "ghost") {
		t.Fatalf("codex config must not carry the background model:\n%s", raw)
	}
}

func TestProviderBackgroundModelValidation(t *testing.T) {
	pm, _, _ := newTestProviderManager(t)
	base := AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models: []string{"m1", "m2"}, ModelContexts: map[string]int{"m1": 128000, "m2": 128000},
	}

	// add：声明必须属于模型集。
	bad := base
	bad.BackgroundModel = "ghost"
	if _, err := pm.AddProvider(bad); err == nil || !strings.Contains(err.Error(), "background model") {
		t.Fatalf("add with ghost background err = %v, want membership error", err)
	}
	good := base
	good.BackgroundModel = "m2"
	if _, err := pm.AddProvider(good); err != nil {
		t.Fatalf("AddProvider() error = %v", err)
	}
	entry, err := pm.GetProvider("main")
	if err != nil || entry.BackgroundModel != "m2" {
		t.Fatalf("stored background = %q, %v; want m2", entry.BackgroundModel, err)
	}

	// edit：未提供（nil）保留原值——把 default 之外的字段全留空验证。
	if _, err := pm.EditProvider(EditProviderOptions{Alias: "main"}); err != nil {
		t.Fatalf("EditProvider() noop error = %v", err)
	}
	if entry, _ := pm.GetProvider("main"); entry.BackgroundModel != "m2" {
		t.Fatalf("edit without the flag must keep m2, got %q", entry.BackgroundModel)
	}

	// edit：缩集导致声明失效时 fail-closed。
	if _, err := pm.EditProvider(EditProviderOptions{Alias: "main", Models: []string{"m1"}}); err == nil ||
		!strings.Contains(err.Error(), "background model") {
		t.Fatalf("shrinking the set below the declaration err = %v, want membership error", err)
	}

	// edit：显式设置与清空。
	if _, err := pm.EditProvider(EditProviderOptions{Alias: "main", BackgroundModel: ptr("m1")}); err != nil {
		t.Fatalf("edit set error = %v", err)
	}
	if entry, _ := pm.GetProvider("main"); entry.BackgroundModel != "m1" {
		t.Fatalf("edit set = %q, want m1", entry.BackgroundModel)
	}
	if _, err := pm.EditProvider(EditProviderOptions{Alias: "main", BackgroundModel: ptr("")}); err != nil {
		t.Fatalf("edit clear error = %v", err)
	}
	if entry, _ := pm.GetProvider("main"); entry.BackgroundModel != "" {
		t.Fatalf("edit clear = %q, want empty", entry.BackgroundModel)
	}
}
