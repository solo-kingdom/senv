# mcp-server-import Specification

## Purpose
把既有 agent 配置文件中的 MCP server 条目批量导入 vault 成为档案，使从其它工具迁移与批量纳管不必逐条手输，且 stdio 与 remote（`http`/`sse`）一视同仁。
## Requirements

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

### Requirement: 传输识别与建档

导入 SHALL 逐条识别传输类型：显式 `type` 为 `http` / `sse` 时按该传输建档；无 `type` 但含 `url` 时按 `http` 建档；含 `command` 时按 `stdio` 建档。`stdio` 条目导入 `command` / `args` / `env`，remote 条目导入 `url` 与 `headers`。无法识别的条目（既无 `url` 也无 `command`，或 `type` 不在支持集合）SHALL 逐条报错并继续处理其余条目。导入值 MUST 原样保存引用模板、不做解析。

#### Scenario: 显式 sse 类型

- **WHEN** 条目为 `{"type": "sse", "url": "https://api.example.com/sse"}`
- **THEN** 建档为 `sse` 传输，`url` 原样保存

#### Scenario: 无类型含 url 按 http

- **WHEN** 条目为 `{"url": "https://api.example.com/mcp", "headers": {"X-Api-Key": "{{env:secrets:KEY}}"}}`
- **THEN** 建档为 `http` 传输，`url` 与 header 值按模板原样保存

#### Scenario: stdio 条目一并导入

- **WHEN** 条目为 `{"command": "npx", "args": ["-y", "@modelcontextprotocol/server-github"], "env": {"GH_TOKEN": "{{env:secrets:T}}"}}`
- **THEN** 建档为 `stdio` 传输，`command` / `args` / `env` 原样保存

#### Scenario: 无法识别条目跳过并继续

- **WHEN** 文件中某条目无 `url` 也无 `command`
- **THEN** 该条目报告失败并说明原因，其余条目正常建档

### Requirement: 冲突处理与结果报告

导入 MUST NOT 覆盖已有档案：别名已存在时该条目 SHALL 标注为冲突跳过且现有档案不变。命令 SHALL 逐条报告每个条目的结果（建档成功 / 冲突跳过 / 失败及原因）并汇总计数；部分失败 MUST NOT 中止其余条目。

#### Scenario: 别名冲突跳过

- **WHEN** vault 已存在档案 github 且导入文件中也有 github 条目
- **THEN** github 条目标注冲突跳过，现有档案字段不变，其余条目正常导入

#### Scenario: 部分失败继续

- **WHEN** 导入 5 个条目其中 1 个无法识别
- **THEN** 其余 4 个成功建档，报告汇总 4 成功、1 失败
