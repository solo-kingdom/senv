package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		"catalog_provider": true, "default_model": true, "models": true,
		"model_info": true,
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

func TestMCPLLMAgentStatusThreeStates(t *testing.T) {
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
			Agent     string `json:"agent"`
			Supported bool   `json:"supported"`
			Pointer   *struct {
				Provider     string `json:"provider"`
				DefaultModel string `json:"default_model"`
			} `json:"pointer"`
			ConfigPath string `json:"config_path"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(textOf(t, res)), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	byAgent := map[string]struct {
		Supported bool
		Pointer   *struct {
			Provider     string `json:"provider"`
			DefaultModel string `json:"default_model"`
		}
	}{}
	for _, a := range payload.Agents {
		byAgent[a.Agent] = struct {
			Supported bool
			Pointer   *struct {
				Provider     string `json:"provider"`
				DefaultModel string `json:"default_model"`
			}
		}{a.Supported, a.Pointer}
	}
	cc := byAgent["claude-code"]
	if !cc.Supported || cc.Pointer == nil || cc.Pointer.Provider != "main" || cc.Pointer.DefaultModel != "m1" {
		t.Fatalf("claude-code status = %+v", cc)
	}
	oc := byAgent["opencode"]
	if !oc.Supported || oc.Pointer != nil {
		t.Fatalf("opencode status = %+v", oc)
	}
	for _, id := range []string{"cursor", "zcode"} {
		if st := byAgent[id]; st.Supported {
			t.Fatalf("%s should be unsupported: %+v", id, st)
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
