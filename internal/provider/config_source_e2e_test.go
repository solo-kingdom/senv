package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wii/senv/internal/storage"
)

// TestE2EConfigSourceProfiles 端到端：机器 A 添加 LLM Provider / MCP Server 档案并推送 →
// 机器 B（全新）首次 pull 拉到档案 → 双端改同一档案产生冲突 → --accept-remote 以远端重建。
func TestE2EConfigSourceProfiles(t *testing.T) {
	baseURL, token, _ := e2eEnv(t)
	ctx := context.Background()
	password := "e2e-config-source"

	// 机器 A：本地 vault + 一条 LLM Provider 档案 + 一条 MCP Server 档案
	cfgA, dataA, keyA := newLocalVault(t, password)
	smA := storage.NewManager(cfgA, dataA)
	now := time.Now()
	if err := smA.SaveLLMProviderWithKey("anthropic", &storage.LLMProviderEntry{
		Alias: "anthropic", BaseURL: "https://api.anthropic.com",
		CredentialRef: "text:llm-keys/anthropic", Models: []string{"claude-sonnet"},
		CreatedAt: now, UpdatedAt: now,
	}, keyA); err != nil {
		t.Fatalf("A SaveLLMProviderWithKey: %v", err)
	}
	if err := smA.SaveMCPServerWithKey("github-mcp", &storage.MCPServerEntry{
		Alias: "github-mcp", Transport: storage.MCPTransportStdio,
		Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-github"},
		CreatedAt: now, UpdatedAt: now,
	}, keyA); err != nil {
		t.Fatalf("A SaveMCPServerWithKey: %v", err)
	}
	pA := NewServerProvider(baseURL, token, cfgA, dataA, "main")
	if _, err := pA.SyncWithReport(ctx); err != nil {
		t.Fatalf("A initial sync: %v", err)
	}

	// 机器 B：全新 bootstrap 接入；bootstrap 即完成首次 pull，配置源档案
	// 直接落地（无 opt-in 闸门，driver D6）
	cfgB, dataB := t.TempDir(), t.TempDir()
	pB := NewServerProvider(baseURL, token, cfgB, dataB, "main")
	if err := pB.Bootstrap(ctx); err != nil {
		t.Fatalf("B bootstrap: %v", err)
	}
	smB := storage.NewManager(cfgB, dataB)
	keyB := deriveKey(t, smB, password)
	llm, err := smB.LoadLLMProviderWithKey("anthropic", keyB)
	if err != nil {
		t.Fatalf("B LoadLLMProviderWithKey: %v", err)
	}
	if llm.BaseURL != "https://api.anthropic.com" || llm.CredentialRef != "text:llm-keys/anthropic" {
		t.Errorf("B llm profile = %+v", llm)
	}
	mcp, err := smB.LoadMCPServerWithKey("github-mcp", keyB)
	if err != nil {
		t.Fatalf("B LoadMCPServerWithKey: %v", err)
	}
	if mcp.Transport != storage.MCPTransportStdio || mcp.Command != "npx" {
		t.Errorf("B mcp profile = %+v", mcp)
	}
	// list 视角可见（与 senv ai provider list / senv mcp list 同一来源）
	llmNames, err := smB.ListLLMProviders()
	if err != nil || len(llmNames) != 1 || llmNames[0] != "anthropic" {
		t.Errorf("B ListLLMProviders = %v, err = %v", llmNames, err)
	}
	mcpNames, err := smB.ListMCPServers()
	if err != nil || len(mcpNames) != 1 || mcpNames[0] != "github-mcp" {
		t.Errorf("B ListMCPServers = %v, err = %v", mcpNames, err)
	}

	// 冲突：A 与 B 基于同一 revision 各自修改同一档案
	now2 := time.Now()
	if err := smA.SaveLLMProviderWithKey("anthropic", &storage.LLMProviderEntry{
		Alias: "anthropic", BaseURL: "https://a-proxy.internal",
		CredentialRef: "text:llm-keys/anthropic", Models: []string{"claude-sonnet"},
		CreatedAt: now, UpdatedAt: now2,
	}, keyA); err != nil {
		t.Fatalf("A update profile: %v", err)
	}
	if _, err := pA.SyncWithReport(ctx); err != nil {
		t.Fatalf("A second sync: %v", err)
	}
	if err := smB.SaveLLMProviderWithKey("anthropic", &storage.LLMProviderEntry{
		Alias: "anthropic", BaseURL: "https://b-proxy.internal",
		CredentialRef: "text:llm-keys/anthropic", Models: []string{"claude-sonnet"},
		CreatedAt: now, UpdatedAt: now2,
	}, keyB); err != nil {
		t.Fatalf("B update profile: %v", err)
	}
	_, err = pB.SyncWithReport(ctx)
	var conflictErr *SyncConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("B sync err = %v, want SyncConflictError", err)
	}
	found := false
	for _, c := range conflictErr.Conflicts {
		if c.Kind == KindLLMProvider {
			found = true
		}
	}
	if !found {
		t.Errorf("conflict list missing llm_provider entry: %+v", conflictErr.Conflicts)
	}

	// --accept-remote：以远端为准重建，B 的档案回到 A 的版本
	if err := pB.AcceptRemote(ctx); err != nil {
		t.Fatalf("B accept-remote: %v", err)
	}
	llm, err = smB.LoadLLMProviderWithKey("anthropic", keyB)
	if err != nil {
		t.Fatalf("B load after accept-remote: %v", err)
	}
	if llm.BaseURL != "https://a-proxy.internal" {
		t.Errorf("B profile after accept-remote = %q, want remote version", llm.BaseURL)
	}
}
