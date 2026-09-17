## ADDED Requirements

### Requirement: MCP Server 说明上限与 list
MCP Server 档案已有的说明字段 SHALL 遵守 2048 字节上限；超限写入 MUST 拒绝。`senv mcp list`、`senv mcp get` 与 MCP `mcp_server_list` SHALL 包含说明（可为空）。MCP 工具 MUST NOT 提供写入说明的途径。

#### Scenario: list includes description
- **WHEN** 档案 `playwright` 有说明
- **THEN** `senv mcp list` 与 `mcp_server_list` 含该说明，仍不输出 env 值

#### Scenario: oversize description rejected
- **WHEN** 编辑档案说明超过 2048 字节
- **THEN** 保存失败，原档案不变
