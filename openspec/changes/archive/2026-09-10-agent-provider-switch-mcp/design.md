## Context

MCP server 走 stdio：启动时校验 session，请求级经 `guardMCPTool` 鉴权后构造 `managers`（env/text/config/ssh），用毕释放。ssh 工具确立了「显式白名单 view struct」的响应范式。指针文件是本机状态，读取不经 vault（`senv ai status` 先例）。

## Goals / Non-goals

**Goals:**

- 两个只读工具，响应形状与 CLI/TUI 语义一致
- 白名单视图：结构上不可能泄漏凭据明文（档案本无明文，view 再裁剪一层）

**Non-goals:**

- 任何 LLM 写入工具（D10）

## Decisions

### D-A managers 扩展

```go
type managers struct {
    ...
    llm        *llm.ProviderManager
    llmPointer string
    llmHome    string
}
```

`newMCPRequestAuthorizer` 内构造：`llm: llm.NewProviderManagerWithKey(store, key)`、`llmPointer: filepath.Join(configPath, "agent-pointers.json")`（configPath 已是参数）、`llmHome: agentHomeDir()`。release 置 nil。

### D-B 响应视图

`llmProviderView` 显式白名单（alias/base_url/credential_ref/catalog_provider/default_model/models/created_at/updated_at）；`llmAgentStatusView`（agent/name/supported/pointer/config_path），pointer 为 `*llm.AgentPointer`（值语义，未切换 nil → JSON null）。

### D-C llm_agent_status 的鉴权

指针读取本身不需要 vault，但 MCP 工具统一走 `guardMCPTool` 鉴权（模型一致性优先，不为它开旁路）。

## 错误处理策略

- list 失败（vault 异常）→ errResult
- status 的指针损坏沿用 SwitchManager.Status 的 warning 语义：损坏按未切换返回并在 warning 字段附说明

## Risks / Trade-offs

- status 需要 session 而数据本机可得 → 保守一致（D-C），未来可评估旁路

## Migration Plan

纯新增工具；managers 新字段对既有工具零影响。

## Open Questions

（无）
