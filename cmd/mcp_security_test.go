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
	"github.com/wii/senv/internal/search"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

// TestMCPTextToolsRejectLLMKeysReservedGroup 验证 MCP 暴露面无法读写/列举 llm-keys
// 保留组（含大小写变体，防 macOS 等大小写不敏感文件系统绕过），llm_provider_list 的
// 白名单设计不被绕过。底层 text.Manager（CLI/TUI 面）保持全权访问。
func TestMCPTextToolsRejectLLMKeysReservedGroup(t *testing.T) {
	m, _, _ := newManagersForTest(t, "pw")
	ctx := context.Background()

	if err := m.text.inner.Set(llm.LLMKeysGroup, "openai", "sk-live-123"); err != nil {
		t.Fatalf("seed llm key: %v", err)
	}

	for _, variant := range []string{llm.LLMKeysGroup, "LLM-KEYS", "Llm-Keys", "  LLM-KEYS  "} {
		// 直接读保留组（含大小写变体）→ 拒绝
		res, _, err := m.textGet(ctx, nil, envGetInput{Key: variant + ":openai"})
		if err != nil {
			t.Fatalf("textGet(%q): %v", variant, err)
		}
		if !res.IsError {
			t.Fatalf("textGet on %q must fail, got: %s", variant, textOf(t, res))
		}
		if strings.Contains(textOf(t, res), "sk-live-123") {
			t.Fatal("reserved-group rejection must not leak the credential")
		}

		// 写/删/列/建组 → 拒绝
		if res, _, _ := m.textSet(ctx, nil, envSetValueInput{Key: variant + ":evil", Value: "x"}); !res.IsError {
			t.Fatalf("textSet on %q must fail", variant)
		}
		if res, _, _ := m.textDelete(ctx, nil, envKeyInput{Key: variant + ":openai"}); !res.IsError {
			t.Fatalf("textDelete on %q must fail", variant)
		}
		if res, _, _ := m.textList(ctx, nil, listInput{Group: variant}); !res.IsError {
			t.Fatalf("textList on %q must fail", variant)
		}
		if res, _, _ := m.groupAdd(ctx, nil, groupKindInput{Kind: "text", Name: variant, Description: "x"}); !res.IsError {
			t.Fatalf("groupAdd on %q must fail", variant)
		}
	}

	// 无 group 的全量 text_list / group_list(text) / senv_search 均不得暴露 llm-keys
	listRes, _, err := m.textList(ctx, nil, listInput{})
	if err != nil || listRes.IsError {
		t.Fatalf("textList all: err=%v res=%s", err, textOf(t, listRes))
	}
	if strings.Contains(textOf(t, listRes), llm.LLMKeysGroup) || strings.Contains(textOf(t, listRes), "openai") {
		t.Fatalf("textList all leaked llm-keys metadata: %s", textOf(t, listRes))
	}
	grpRes, _, err := m.groupList(ctx, nil, listInput{Group: "text"})
	if err != nil || grpRes.IsError {
		t.Fatalf("groupList text: err=%v res=%s", err, textOf(t, grpRes))
	}
	if strings.Contains(textOf(t, grpRes), llm.LLMKeysGroup) {
		t.Fatalf("groupList text leaked llm-keys: %s", textOf(t, grpRes))
	}
	searchRes, _, err := m.search(ctx, nil, searchInput{Query: "openai"})
	if err != nil || searchRes.IsError {
		t.Fatalf("search: err=%v res=%s", err, textOf(t, searchRes))
	}
	var searchOut search.Result
	if err := json.Unmarshal([]byte(textOf(t, searchRes)), &searchOut); err != nil {
		t.Fatalf("unmarshal search result: %v", err)
	}
	if searchOut.Total != 0 || len(searchOut.Hits) != 0 {
		t.Fatalf("senv_search leaked llm-keys entry: %+v", searchOut)
	}

	// 凭据未被破坏，CLI 面仍可读
	v, err := m.text.inner.Get(llm.LLMKeysGroup, "openai")
	if err != nil || v != "sk-live-123" {
		t.Fatalf("llm key must be intact after denied MCP access (v=%q err=%v)", v, err)
	}
}

// TestMCPReferenceDecodeRejectsLLMKeys 验证 decode=true 与 env_export 的引用解析
// 无法用 {{text:llm-keys:...}} 或大小写变体 {{text:LLM-KEYS:...}} 借道取回保留组凭据。
func TestMCPReferenceDecodeRejectsLLMKeys(t *testing.T) {
	m, _, _ := newManagersForTest(t, "pw")
	ctx := context.Background()

	if err := m.text.inner.Set(llm.LLMKeysGroup, "openai", "sk-live-123"); err != nil {
		t.Fatalf("seed llm key: %v", err)
	}

	if err := m.text.inner.Set("default", "public", "ok-val"); err != nil {
		t.Fatalf("seed normal text: %v", err)
	}
	if _, _, err := m.envSet(ctx, nil, envSetValueInput{Key: "NORMAL", Value: "{{text:default:public}}"}); err != nil {
		t.Fatal(err)
	}
	okRes, _, err := m.envGet(ctx, nil, envGetInput{Key: "NORMAL", Decode: true})
	if err != nil || okRes.IsError || !strings.Contains(textOf(t, okRes), "ok-val") {
		t.Fatalf("sanity check normal text ref decode failed: err=%v res=%s", err, textOf(t, okRes))
	}

	for i, expr := range []string{"{{text:llm-keys:openai}}", "{{text:LLM-KEYS:openai}}"} {
		key := "PIVOT_" + string(rune('A'+i))
		if _, _, err := m.envSet(ctx, nil, envSetValueInput{Key: key, Value: expr}); err != nil {
			t.Fatal(err)
		}

		res, _, err := m.envGet(ctx, nil, envGetInput{Key: key, Decode: true})
		if err != nil {
			t.Fatalf("envGet decode(%s): %v", expr, err)
		}
		if !res.IsError {
			t.Fatalf("envGet decode(%s) must return tool error, got: %s", expr, textOf(t, res))
		}
		body := textOf(t, res)
		if strings.Contains(body, "sk-live-123") {
			t.Fatalf("decode(%s) resolved a reserved-group credential: %s", expr, body)
		}
	}

	// senv_env_export 自动解引用也不得解析出 llm-keys 明文
	expRes, _, err := m.envExport(ctx, nil, struct{}{})
	if err != nil {
		t.Fatalf("envExport: %v", err)
	}
	if strings.Contains(textOf(t, expRes), "sk-live-123") {
		t.Fatalf("envExport leaked reserved-group credential: %s", textOf(t, expRes))
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
		text: mcpTextManager{inner: text.NewManager(store, "pw")},
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
