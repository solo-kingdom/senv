## MODIFIED Requirements

### Requirement: 格式映射与合并

导出 SHALL 按目标 agent 的配置格式写入：JSON 族写 `<serversKey>.<alias>` 对象，TOML 族写 `[mcp_servers.<alias>]` 表。`stdio` 档案只落跨 agent 公共子集 `command` / `args` / `env`，`command` MUST 原样写入，不做路径归一。`http` / `sse` 档案 SHALL 按目标 agent 的 remote 键名矩阵写入其文档化键集（传输类型、`url`、`headers`），`url` 与 header 值 MUST 为导出时解析后的明文；MUST NOT 透传 agent 特有键。写入 MUST 保留配置中的其它键与其它 MCP server。目标 agent 的配置格式无法表达该传输时，SHALL 将该 (agent, alias) 条目标注 `error` 并说明原因，MUST NOT 静默跳过或写入不可用配置。

#### Scenario: 保留其它 server

- **WHEN** agent 配置已存在其它 MCP server 条目
- **THEN** 导出后这些条目原样保留

#### Scenario: command 原样写入

- **WHEN** 档案 `command` 为 `npx`
- **THEN** 写入的值仍为 `npx`，不被替换为绝对路径

#### Scenario: 不透传 agent 特有键

- **WHEN** 档案不含 `disabled` / `autoApprove` 之类字段
- **THEN** 导出结果中不出现此类键

#### Scenario: remote 档案写入 JSON 族

- **WHEN** 导出 `http` 档案到 claude-code
- **THEN** `<serversKey>.<alias>` 写入传输类型 `http`、明文 `url` 与 `headers`，不出现 `command` / `args`

#### Scenario: remote 档案写入 TOML 族

- **WHEN** 导出 `http` 档案到 codex
- **THEN** `[mcp_servers.<alias>]` 表按 codex 的 remote 键名矩阵写入，既有表内容保留

#### Scenario: 目标不支持该传输报错

- **WHEN** 某 agent 的配置格式无法表达 `sse` 档案时导出该档案
- **THEN** 计划中该条目标注 `error` 并说明原因，该 agent 的文件不被修改，其余 agent 继续

### Requirement: 明文落盘提示

导出计划 MUST 标注哪些条目会把明文值（`env`、`url`、`headers`）写入哪个文件。写入内容中的 `env`、`url` 与 header 值 SHALL 为导出时解析后的明文；任一引用无法解析时（严格模式）SHALL 报错并终止该 agent 的写入。

#### Scenario: 计划标注明文

- **WHEN** 某 remote 档案含 headers 且目标 agent 将被写入
- **THEN** 计划中该条目标注会写入明文（含 headers）及其目标路径

#### Scenario: 引用解析失败

- **WHEN** 档案 `url` 引用 `{{env:secrets:MISSING}}` 且该 key 不存在
- **THEN** 报错，该 agent 的目标文件不被修改
