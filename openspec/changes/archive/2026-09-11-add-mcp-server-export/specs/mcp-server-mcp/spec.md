## Purpose

让宿主 Coding Agent 通过 MCP 只读感知 senv 中已保存的 MCP Server 档案，用于建议与排障。

## ADDED Requirements

### Requirement: 只读工具 mcp_server_list

MCP 工具面 SHALL 提供只读工具 `mcp_server_list`，返回每条档案的别名、传输类型与描述，按别名稳定排序；MUST NOT 返回 `env` / `args` 等可能含机密的字段。无档案时 SHALL 返回空数组。

#### Scenario: 列出档案不含值

- **WHEN** 存在含 `env` 值的档案时调用 `mcp_server_list`
- **THEN** 结果包含别名、传输类型与描述，不含任何 `env` 值或参数

#### Scenario: 空档案集

- **WHEN** vault 中没有任何 MCP Server 档案
- **THEN** 返回空数组而非错误

### Requirement: 不提供写侧工具

MCP 工具面 MUST NOT 提供导出、撤回或档案写入类工具；导出属本机文件写操作，只经 CLI 暴露。

#### Scenario: 工具清单无写侧工具

- **WHEN** 执行 `senv mcp list-tools`
- **THEN** 清单中包含 `mcp_server_list`，不含任何 export / unexport / add / edit 类工具

### Requirement: 鉴权沿用既有请求守卫

`mcp_server_list` SHALL 与其它只读工具一样经过既有 MCP 请求鉴权；无有效 session 时 SHALL 返回错误而非数据。

#### Scenario: 无 session 拒绝

- **WHEN** 在无有效 session 的情况下调用 `mcp_server_list`
- **THEN** 返回鉴权错误，不返回任何档案数据
