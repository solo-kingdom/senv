package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/llm"
)

func setupLLMMCPTest(t *testing.T) *managers {
	t.Helper()
	newAuditTestProject(t)
	addAIProviderForSwitchTest(t, "main")
	home := t.TempDir()
	t.Setenv("HOME", home)
	llmManager, err := getAIProviderManager()
	if err != nil {
		t.Fatal(err)
	}
	return &managers{
		llm:        llmManager,
		llmPointer: filepath.Join(getConfigPath(), "agent-pointers.json"),
		llmHome:    home,
		autoPull:   func() {},
	}
}

func TestMCPLLMProviderListWhitelistedOnly(t *testing.T) {
	requestManagers := setupLLMMCPTest(t)
	res, _, err := requestManagers.llmProviderList(context.Background(), nil, struct{}{})
	if err != nil || res.IsError {
		t.Fatalf("llm_provider_list = %v, %v", res, err)
	}
	text := textOf(t, res)
	var providers []map[string]any
	if err := json.Unmarshal([]byte(text), &providers); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("providers = %d, want 1", len(providers))
	}
	allowed := map[string]bool{
		"alias": true, "base_url": true, "credential_ref": true,
		"catalog_provider": true, "default_model": true, "background_model": true,
		"models": true,
		"model_info": true, "api_shape": true, "shape_urls": true,
		"created_at": true, "updated_at": true,
	}
	for key := range providers[0] {
		if !allowed[key] {
			t.Fatalf("unexpected response field %q", key)
		}
	}
	if providers[0]["alias"] != "main" || providers[0]["credential_ref"] != "text:llm-keys/main" {
		t.Fatalf("provider view = %v", providers[0])
	}
	if strings.Contains(text, "sk-secret-value") {
		t.Fatal("MCP response leaked credential plaintext")
	}
}

// TestMCPLLMProviderListBackgroundModel 覆盖 ADR-0029：档案声明的后台模型
// 出现在只读视图里，未声明时字段省略。
func TestMCPLLMProviderListBackgroundModel(t *testing.T) {
	requestManagers := setupLLMMCPTest(t)
	declared := "m2"
	if _, err := requestManagers.llm.EditProvider(llm.EditProviderOptions{Alias: "main", BackgroundModel: &declared}); err != nil {
		t.Fatalf("EditProvider: %v", err)
	}
	res, _, err := requestManagers.llmProviderList(context.Background(), nil, struct{}{})
	if err != nil || res.IsError {
		t.Fatalf("llm_provider_list = %v, %v", res, err)
	}
	var providers []map[string]any
	if err := json.Unmarshal([]byte(textOf(t, res)), &providers); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(providers) != 1 || providers[0]["background_model"] != "m2" {
		t.Fatalf("provider view = %v, want background_model m2", providers)
	}
}

func TestMCPLLMProviderListEmpty(t *testing.T) {
	newAuditTestProject(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	llmManager, err := getAIProviderManager()
	if err != nil {
		t.Fatal(err)
	}
	requestManagers := &managers{llm: llmManager, llmHome: home, autoPull: func() {}}
	res, _, err := requestManagers.llmProviderList(context.Background(), nil, struct{}{})
	if err != nil || res.IsError {
		t.Fatalf("llm_provider_list = %v, %v", res, err)
	}
	if got := strings.TrimSpace(textOf(t, res)); got != "[]" {
		t.Fatalf("empty list = %q, want []", got)
	}
}

func TestMCPLLMProviderModelInfoNewFields(t *testing.T) {
	newAuditTestProject(t)
	setProviderCredentialReader(t, "sk-secret-value")
	setProviderAddFlags(t, func() {
		providerAddBaseURL = "https://api.example.com"
		providerAddModels = []string{"m1"}
		providerAddModelCtx = []string{"m1=128000"}
		providerAddModelReason = []string{"m1=low;high"}
		providerAddModelDefaultReason = []string{"m1=high"}
		providerAddModelModalities = []string{"m1=text,image"}
	})
	if _, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"main"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	llmManager, err := getAIProviderManager()
	if err != nil {
		t.Fatal(err)
	}
	requestManagers := &managers{llm: llmManager, autoPull: func() {}}
	res, _, err := requestManagers.llmProviderList(context.Background(), nil, struct{}{})
	if err != nil || res.IsError {
		t.Fatalf("llm_provider_list = %v, %v", res, err)
	}
	var providers []map[string]any
	if err := json.Unmarshal([]byte(textOf(t, res)), &providers); err != nil {
		t.Fatalf("decode: %v", err)
	}
	info, ok := providers[0]["model_info"].(map[string]any)
	if !ok {
		t.Fatalf("model_info missing: %v", providers[0])
	}
	m1 := info["m1"].(map[string]any)
	if m1["default_reasoning"] != "high" {
		t.Fatalf("default_reasoning = %v", m1["default_reasoning"])
	}
	mods := m1["input_modalities"].([]any)
	if len(mods) != 2 || mods[0] != "text" || mods[1] != "image" {
		t.Fatalf("input_modalities = %v", mods)
	}
	if strings.Contains(textOf(t, res), "sk-secret-value") {
		t.Fatal("MCP response leaked credential plaintext")
	}
}

func TestMCPLLMAgentStatusTwoStates(t *testing.T) {
	requestManagers := setupLLMMCPTest(t)
	// 先经 CLI 切换 claude-code，制造已切换状态。
	t.Setenv("HOME", requestManagers.llmHome)
	if _, _, err := runAISwitchCmd(t, aiSwitchCmd, []string{"claude-code", "main"}); err != nil {
		t.Fatalf("switch: %v", err)
	}

	res, _, err := requestManagers.llmAgentStatus(context.Background(), nil, struct{}{})
	if err != nil || res.IsError {
		t.Fatalf("llm_agent_status = %v, %v", res, err)
	}
	var payload struct {
		Agents []struct {
			Agent   string `json:"agent"`
			Pointer *struct {
				Provider     string `json:"provider"`
				DefaultModel string `json:"default_model"`
			} `json:"pointer"`
			ConfigPath string `json:"config_path"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(textOf(t, res)), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	byAgent := map[string]*struct {
		Provider     string `json:"provider"`
		DefaultModel string `json:"default_model"`
	}{}
	for _, a := range payload.Agents {
		byAgent[a.Agent] = a.Pointer
	}
	if p := byAgent["claude-code"]; p == nil || p.Provider != "main" || p.DefaultModel != "m1" {
		t.Fatalf("claude-code status = %+v", p)
	}
	if p := byAgent["opencode"]; p != nil {
		t.Fatalf("opencode status = %+v", p)
	}
	for _, id := range []string{"cursor", "zcode"} {
		if _, ok := byAgent[id]; ok {
			t.Fatalf("%s must not appear in agent status: %+v", id, payload.Agents)
		}
	}
	// 确认指针文件真实存在且状态来自本机（不依赖 vault）。
	if _, err := os.Stat(requestManagers.llmPointer); err != nil {
		t.Fatalf("pointer file missing: %v", err)
	}
}

func TestMCPCatalogueIncludesLLMReadOnlyTools(t *testing.T) {
	catalogue := toolCatalogue()
	names := map[string]bool{}
	for _, tl := range catalogue {
		names[tl.Name] = true
	}
	if !names["llm_provider_list"] || !names["llm_agent_status"] {
		t.Fatal("catalogue missing LLM read-only tools")
	}
	for _, tl := range catalogue {
		if strings.HasPrefix(tl.Name, "llm_") && tl.Name != "llm_provider_list" && tl.Name != "llm_agent_status" {
			t.Fatalf("unexpected LLM tool %q (write tools are forbidden)", tl.Name)
		}
	}
}

func TestMCPLLMProviderListShapeURLs(t *testing.T) {
	newAuditTestProject(t)
	setProviderCredentialReader(t, "sk-secret-value")
	setProviderAddFlags(t, func() {
		providerAddBaseURL = "https://api.example.com"
		providerAddModels = []string{"m1"}
		providerAddModelCtx = []string{"m1=128000"}
		providerAddAPIShape = "openai-chat"
		providerAddShapeURLs = []string{"anthropic=https://gw.example.com/api/anthropic"}
	})
	if _, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"main"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	llmManager, err := getAIProviderManager()
	if err != nil {
		t.Fatal(err)
	}
	requestManagers := &managers{llm: llmManager, autoPull: func() {}}
	res, _, err := requestManagers.llmProviderList(context.Background(), nil, struct{}{})
	if err != nil || res.IsError {
		t.Fatalf("llm_provider_list = %v, %v", res, err)
	}
	var providers []map[string]any
	if err := json.Unmarshal([]byte(textOf(t, res)), &providers); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if providers[0]["api_shape"] != "openai-chat" {
		t.Fatalf("api_shape = %v", providers[0]["api_shape"])
	}
	shapeURLs, ok := providers[0]["shape_urls"].(map[string]any)
	if !ok {
		t.Fatalf("shape_urls = %v, want object", providers[0]["shape_urls"])
	}
	if shapeURLs["anthropic"] != "https://gw.example.com/api/anthropic" {
		t.Fatalf("shape_urls.anthropic = %v", shapeURLs["anthropic"])
	}
	if _, has := shapeURLs["chat"]; has {
		t.Fatal("unset chat shape URL must be omitted")
	}
	if strings.Contains(textOf(t, res), "sk-secret-value") {
		t.Fatal("MCP response leaked credential plaintext")
	}

	// 未声明形态的档案：api_shape 与 shape_urls 键省略。
	setProviderCredentialReader(t, "k2")
	setProviderAddFlags(t, func() {
		providerAddBaseURL = "https://plain.example.com"
		providerAddModels = []string{"m1"}
		providerAddModelCtx = []string{"m1=128000"}
		providerAddAPIShape = ""
		providerAddShapeURLs = nil
	})
	if _, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"plain"}); err != nil {
		t.Fatalf("add plain: %v", err)
	}
	res, _, err = requestManagers.llmProviderList(context.Background(), nil, struct{}{})
	if err != nil || res.IsError {
		t.Fatalf("llm_provider_list = %v, %v", res, err)
	}
	providers = nil
	if err := json.Unmarshal([]byte(textOf(t, res)), &providers); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, p := range providers {
		if p["alias"] != "plain" {
			continue
		}
		if _, has := p["api_shape"]; has {
			t.Fatalf("undeclared api_shape must be omitted: %v", p["api_shape"])
		}
		if _, has := p["shape_urls"]; has {
			t.Fatal("empty shape_urls must be omitted")
		}
	}
}
