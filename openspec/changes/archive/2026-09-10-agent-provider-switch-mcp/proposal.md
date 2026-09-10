## Why

driver `agent-provider-switch-driver` 的最后一个子 change：CLI 与 TUI 已覆盖人工操作，AI agent 需要通过 MCP 以只读方式查询 provider 档案与各 coding agent 当前指向（D9/D10 约定 MCP 只读），供 agent 在对话中感知与建议。

## What Changes

- `managers` 扩展 LLM 管理器字段与指针/ home 配置；请求级构造与释放对齐既有模式
- 新增 `cmd/mcp_llm.go`：`llm_provider_list`（档案白名单视图，无凭据明文）与 `llm_agent_status`（各 agent 当前指向）两个只读工具
- `registerMCPTools` 与 `toolCatalogue` 同步注册

## Non-goals

- 不提供任何切换/写入类 MCP 工具（D10 只读）
- 不在 MCP 响应中输出凭据明文（档案本身只存引用）

## Capabilities

### New Capabilities

- `llm-provider-mcp`: MCP 只读查询 LLM provider 档案与 coding agent 当前指向

### Modified Capabilities

（无）

## Impact

- `cmd/mcp.go`：managers 扩展、authorizer 构造/释放、工具注册与 catalogue
- `cmd/mcp_llm.go` 新增；`cmd/mcp_test.go` 或新增 `cmd/mcp_llm_test.go` 覆盖

## 验证记录

- 2026-09-10：`go test -race ./cmd`（llm_provider_list 白名单/无明文/空数组、llm_agent_status 三态、catalogue 一致）全部通过；`make check`（fmt + vet + lint + go test -race ./...）全部通过；`openspec validate agent-provider-switch-mcp --strict` 通过。
