## MODIFIED Requirements

### Requirement: 导入命令与输入

`senv mcp import <file>` SHALL 读取指定的 agent 配置文件并将其中的 MCP server 条目建档。文件 MUST 为 JSON 族（含 `mcpServers` 键的对象）或 Codex TOML（`[mcp_servers.<alias>]` 表）。文件不存在或解析失败时 SHALL 报错且不创建任何档案。`--dry-run` SHALL 只输出将执行的建档计划，不落库。

TUI MCP Tab SHALL 提供 `i`（import）键作为同一能力的入口：按键后 SHALL 弹出单字段路径表单（接受 `~` 展开），提交后按与 CLI 完全相同的文件解析、传输识别与冲突跳过语义建档；执行完成 SHALL 弹出结果报告，逐条列出每个条目的结果（建档成功含传输类型 / 冲突跳过 / 失败及原因）并汇总新建、冲突跳过、失败计数，报告 MUST NOT 包含任何值（url/header/env 值不渲染）。TUI 入口不提供 `--dry-run`（保留在 CLI）；文件不存在或解析失败时 SHALL 报错且 vault 零变更，与 CLI 一致。

#### Scenario: 导入 JSON 配置

- **WHEN** 对含 `mcpServers` 的 JSON 文件执行 `senv mcp import config.json`
- **THEN** 文件中每个 server 条目被建档并逐条报告结果

#### Scenario: 导入 Codex TOML

- **WHEN** 对含 `[mcp_servers.<alias>]` 表的 TOML 文件执行 `senv mcp import config.toml`
- **THEN** 文件中每个 server 表被建档并逐条报告结果

#### Scenario: 文件解析失败不落库

- **WHEN** 对非法 JSON 文件执行 `senv mcp import bad.json`
- **THEN** 报错说明解析失败，vault 中不出现任何新档案

#### Scenario: dry-run 不落库

- **WHEN** 执行 `senv mcp import config.json --dry-run`
- **THEN** 输出逐条建档计划，vault 无任何变化

#### Scenario: TUI 路径表单导入并出结果报告

- **WHEN** 用户在 MCP Tab 按 `i`，在路径表单填入 `~/.claude.json` 并提交
- **THEN** 系统 SHALL 按与 CLI 相同的传输识别与冲突跳过语义建档，弹出结果报告逐条列出 create/conflict/failed 及原因，底部汇总新建、冲突跳过、失败计数，报告不含任何值，档案列表刷新

#### Scenario: TUI 导入冲突不覆盖

- **WHEN** 导入文件中某别名已存在于 vault，用户在 TUI 完成导入
- **THEN** 该条目在结果报告中标注冲突跳过，现有档案字段不变，其余条目正常建档

#### Scenario: TUI 导入解析失败不落库

- **WHEN** 用户在 TUI 路径表单提交一个不存在的路径或非法 JSON
- **THEN** 系统 SHALL 报错且 vault 零变更，不产生任何新档案
