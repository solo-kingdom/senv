## 1. 工具实现

- [x] 1.1 `cmd/mcp.go`：managers 扩展 llm/llmPointer/llmHome；authorizer 构造与释放；`registerMCPTools`/`toolCatalogue` 注册 `llm_provider_list`、`llm_agent_status`
- [x] 1.2 `cmd/mcp_llm.go`：`llmProviderView` 白名单视图 + `llm_provider_list` handler；`llmAgentStatusView` + `llm_agent_status` handler（warning 透传）

## 2. 测试与收尾

- [x] 2.1 工具测试：list 白名单字段/无明文/空数组；status 三态与 warning；catalogue 一致（list-tools 输出含新工具、无写入工具）
- [x] 2.2 `make check` 全绿；`openspec validate agent-provider-switch-mcp --strict` 通过；回填 proposal 验证记录
