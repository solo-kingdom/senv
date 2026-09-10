## Purpose

把 vault 中的 MCP Server 档案按各 Coding Agent 的配置格式合并写入其全局配置，并提供撤回与漂移处理。

## ADDED Requirements

### Requirement: 导出目标选择

`senv mcp export` MUST 显式指定目标：`--agent <id>[,<id>...]` 或 `--all`。未指定目标时 SHALL 报错并列出支持的 agent id；指定未知 agent 时 SHALL 报错且不产生任何写入。导出目标 SHALL 为 agent 的 user 级全局配置文件。

#### Scenario: 未指定目标报错

- **WHEN** 执行 `senv mcp export` 且未给 `--agent` 或 `--all`
- **THEN** 报错并列出支持的 agent id，不写任何文件

#### Scenario: 未知 agent 报错

- **WHEN** 执行 `senv mcp export --agent nosuchagent`
- **THEN** 报错，且所有目标文件保持不变

### Requirement: 导出计划与确认

导出 SHALL 先输出操作计划，逐条列出 agent、目标路径、别名、动作（`create` / `update` / `skip` / `drift` / `error`）及原因；SHALL 在用户确认后才写入。`--dry-run` SHALL 只输出计划，`--print` SHALL 只输出片段而不落盘。

#### Scenario: dry-run 不落盘

- **WHEN** 执行 `senv mcp export --all --dry-run`
- **THEN** 输出完整计划，文件系统无任何变化

#### Scenario: 内容一致跳过

- **WHEN** 目标 agent 配置中的条目与 senv 将要写入的内容相同
- **THEN** 计划中该条目标注 `skip`，不写入、不备份

### Requirement: 格式映射与合并

导出 SHALL 按目标 agent 的配置格式写入：JSON 族写 `<serversKey>.<alias>` 对象，TOML 族写 `[mcp_servers.<alias>]` 表。写入 MUST 保留配置中的其它键与其它 MCP server，只落跨 agent 公共子集 `command` / `args` / `env`；`command` MUST 原样写入，不做路径归一。

#### Scenario: 保留其它 server

- **WHEN** agent 配置已存在其它 MCP server 条目
- **THEN** 导出后这些条目原样保留

#### Scenario: command 原样写入

- **WHEN** 档案 `command` 为 `npx`
- **THEN** 写入的值仍为 `npx`，不被替换为绝对路径

#### Scenario: 不透传 agent 特有键

- **WHEN** 档案不含 `disabled` / `autoApprove` 之类字段
- **THEN** 导出结果中不出现此类键

### Requirement: 备份与文件权限

写入前，若目标文件已存在且内容将变化，SHALL 先备份为同目录的 `<file>.bak`。目标文件 SHALL 以 0600 写入；父目录缺失时 SHALL 递归创建。

#### Scenario: 覆盖前备份

- **WHEN** 目标 agent 配置文件已存在且需要更新条目
- **THEN** 先生成 `<file>.bak` 再写入

### Requirement: 明文落盘提示

导出计划 MUST 标注哪些条目会把明文值写入哪个文件。写入内容中的 `env` 值 SHALL 为导出时解析后的明文；引用无法解析时（严格模式）SHALL 报错并终止该 agent 的写入。

#### Scenario: 计划标注明文

- **WHEN** 某档案含 `env` 值且目标 agent 将被写入
- **THEN** 计划中该条目标注会写入明文及其目标路径

#### Scenario: 引用解析失败

- **WHEN** 档案值引用 `{{env:secrets:MISSING}}` 且该 key 不存在
- **THEN** 报错，该 agent 的目标文件不被修改

### Requirement: 漂移判定与覆盖

导出 SHALL 在本机维护台账 `~/.config/senv/mcp-exports.json`，记录 agent、别名与写入内容指纹。目标条目的实际内容与台账指纹不一致时 SHALL 判为漂移，默认拒绝覆盖；仅当显式 `--force` 时才覆盖。同名条目无台账记录时 SHALL 视为外部条目，同样只在 `--force` 下覆盖。

#### Scenario: 漂移默认拒绝

- **WHEN** 目标条目被手工修改过，台账指纹不匹配，且未给 `--force`
- **THEN** 该条目标注 `drift` 并跳过，文件不被修改

#### Scenario: force 覆盖漂移

- **WHEN** 同样场景下给出 `--force`
- **THEN** 条目被覆盖，备份与台账同步更新

### Requirement: 部分失败与台账更新

单个 agent 写入失败 MUST NOT 中止其余 agent 的导出。命令 SHALL 逐条报告成功与失败；台账 SHALL 只记录写入成功的条目。

#### Scenario: 一个目标失败其余继续

- **WHEN** 导出 7 个 agent 时其中一个目标文件不可写
- **THEN** 其余 agent 正常导出，失败项以非零结果报告，台账不含失败项

### Requirement: 撤回导出

`senv mcp unexport` SHALL 删除目标 agent 配置中由 senv 导出的条目：条目内容与 senv 期望一致时 SHALL 直接删除；被本地修改过时 SHALL 要求确认后才删除。删除档案 MUST NOT 自动触发撤回。

#### Scenario: 内容一致直接删除

- **WHEN** 目标条目与 senv 期望内容一致时执行 `senv mcp unexport`
- **THEN** 该条目被删除，配置中其它内容保留

#### Scenario: 被改过需确认

- **WHEN** 目标条目被本地修改过时执行 `senv mcp unexport`
- **THEN** 提示该条已被修改，确认后才删除
