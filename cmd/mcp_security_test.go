package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

// TestMCPTextToolsRejectLLMKeysReservedGroup 验证 MCP 暴露面无法读写 llm-keys
// 保留组（LLM API key 明文凭据），llm_provider_list 的白名单设计不被绕过。
// 底层 text.Manager（CLI/TUI 面）保持全权访问。
func TestMCPTextToolsRejectLLMKeysReservedGroup(t *testing.T) {
	m, _, _ := newManagersForTest(t, "pw")
	ctx := context.Background()

	if err := m.text.Manager.Set(llm.LLMKeysGroup, "openai", "sk-live-123"); err != nil {
		t.Fatalf("seed llm key: %v", err)
	}

	// 直接读保留组 → 拒绝
	res, _, err := m.textGet(ctx, nil, envGetInput{Key: llm.LLMKeysGroup + ":openai"})
	if err != nil {
		t.Fatalf("textGet: %v", err)
	}
	if !res.IsError {
		t.Fatalf("textGet on llm-keys must fail, got: %s", textOf(t, res))
	}
	if strings.Contains(textOf(t, res), "sk-live-123") {
		t.Fatal("reserved-group rejection must not leak the credential")
	}

	// 写/删保留组 → 拒绝
	if res, _, _ := m.textSet(ctx, nil, envSetValueInput{Key: llm.LLMKeysGroup + ":evil", Value: "x"}); !res.IsError {
		t.Fatal("textSet on llm-keys must fail")
	}
	if res, _, _ := m.textDelete(ctx, nil, envKeyInput{Key: llm.LLMKeysGroup + ":openai"}); !res.IsError {
		t.Fatal("textDelete on llm-keys must fail")
	}

	// 凭据未被破坏，CLI 面仍可读
	v, err := m.text.Manager.Get(llm.LLMKeysGroup, "openai")
	if err != nil || v != "sk-live-123" {
		t.Fatalf("llm key must be intact after denied MCP access (v=%q err=%v)", v, err)
	}
}

// TestMCPReferenceDecodeRejectsLLMKeys 验证 decode=true 的引用解析无法用
// {{text:llm-keys/...}} 借道取回保留组凭据。
func TestMCPReferenceDecodeRejectsLLMKeys(t *testing.T) {
	m, _, _ := newManagersForTest(t, "pw")
	ctx := context.Background()

	if err := m.text.Manager.Set(llm.LLMKeysGroup, "openai", "sk-live-123"); err != nil {
		t.Fatalf("seed llm key: %v", err)
	}
	if _, _, err := m.envSet(ctx, nil, envSetValueInput{Key: "PIVOT", Value: "{{text:llm-keys/openai}}"}); err != nil {
		t.Fatal(err)
	}

	res, _, err := m.envGet(ctx, nil, envGetInput{Key: "PIVOT", Decode: true})
	if err != nil {
		t.Fatalf("envGet decode: %v", err)
	}
	body := textOf(t, res)
	if strings.Contains(body, "sk-live-123") {
		t.Fatalf("decode resolved a reserved-group credential: %s", body)
	}
}

// TestMCPToolCallAudited 验证经 guard 的工具调用留下 op_mcp_tool 审计事件
// （成功与工具级失败都留痕，且不包含任何值）。
func TestMCPToolCallAudited(t *testing.T) {
	// 审计日志路径基于 HOME；隔离到临时目录
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "cfg")
	dataPath := filepath.Join(dir, "data")
	for _, p := range []string{configPath, dataPath} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	store := storage.NewManager(configPath, dataPath)
	if err := store.Initialize("pw"); err != nil {
		t.Fatal(err)
	}

	requestManagers := &managers{
		env:  env.NewManager(store, "pw"),
		text: mcpTextManager{text.NewManager(store, "pw")},
	}
	authorize := func() (*managers, func(), error) { return requestManagers, func() {}, nil }

	if err := requestManagers.env.Set("default", "API_KEY", "secret"); err != nil {
		t.Fatal(err)
	}
	get := guardMCPTool("senv_env_get", authorize, nil, (*managers).envGet)
	if res, _, err := get(context.Background(), nil, envGetInput{Key: "API_KEY"}); err != nil || res.IsError {
		t.Fatalf("successful call failed: %v %s", err, textOf(t, res))
	}
	// 工具级失败（key 不存在）也留痕
	if res, _, _ := get(context.Background(), nil, envGetInput{Key: "MISSING"}); !res.IsError {
		t.Fatal("missing key should be a tool-level error")
	}

	entries := readAuditForTest(t)
	var okCount, failCount int
	for _, e := range entries {
		if e.EventType != session.AuditOpMCPTool || e.Target != "mcp:senv_env_get" {
			continue
		}
		if strings.Contains(e.Message, "API_KEY") && strings.Contains(e.Message, "value") {
			t.Fatalf("audit message must not contain values: %+v", e)
		}
		if e.Success {
			okCount++
		} else {
			failCount++
		}
	}
	if okCount == 0 || failCount == 0 {
		t.Fatalf("want audited success+failure, got ok=%d fail=%d (entries=%d)", okCount, failCount, len(entries))
	}
}

// readAuditForTest 读取 HOME 下的审计日志。
func readAuditForTest(t *testing.T) []session.AuditEntry {
	t.Helper()
	data, err := os.ReadFile(session.AuditLogPath())
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	var entries []session.AuditEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e session.AuditEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries
}
