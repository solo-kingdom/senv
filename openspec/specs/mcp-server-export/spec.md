# mcp-server-export Specification

## Purpose
把 vault 中的 MCP Server 档案按各 Coding Agent 的配置格式合并写入其全局配置，并提供撤回与漂移处理。
## Requirements

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

导出 SHALL 按目标 agent 的配置格式写入：JSON 族写 `<serversKey>.<alias>` 对象，TOML 族写 `[mcp_servers.<alias>]` 表。`stdio` 档案只落跨 agent 公共子集 `command` / `args` / `env`，`command` MUST 原样写入，不做路径归一。`http` / `sse` 档案 SHALL 按目标 agent 的 remote 键名矩阵写入其文档化键集（`url`、`headers`，以及该目标确实有文档化的传输类型键时才写该键），`url` 与 header 值 MUST 为导出时解析后的明文；MUST NOT 透传 agent 特有键。写入 MUST 保留配置中的其它键与其它 MCP server。目标 agent 的配置格式无法表达该传输时，SHALL 将该 (agent, alias) 条目标注 `error` 并说明原因，MUST NOT 静默跳过或写入不可用配置。

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

### Requirement: 目标前置依赖提示与自动安装

当目标 agent 需要外部组件才能读取 senv 写入的配置时（例如无内置 MCP 支持、依赖扩展适配器的目标），senv SHALL 在写盘前尝试安装该组件（best-effort）：已装（根据 agent 设置文件判定）则跳过，未装则调用该 agent 的安装器并设超时。安装失败、安装器不在 PATH 或超时 SHALL 只输出提示（含手动安装方式）并继续写盘，MUST NOT 中止写入、MUST NOT 计入导出失败；安装成功也 SHALL 在输出中报告。senv MUST NOT 卸载该组件，也 MUST NOT 让用户把写入成功误认为配置已生效。

#### Scenario: 适配器型目标自动安装前置依赖

- **WHEN** 安装或导出到依赖扩展适配器且该扩展尚未安装的目标（如 pi）
- **THEN** 写盘前尝试安装该扩展并报告结果，随后照常写入配置

#### Scenario: 安装失败不阻断写盘

- **WHEN** 自动安装失败或安装器不在 PATH
- **THEN** 输出失败原因与手动安装命令，配置文件仍被写入，导出结果不计失败

#### Scenario: 已安装则跳过

- **WHEN** agent 设置文件已列出该扩展包
- **THEN** 不调用安装器，不产生额外输出

### Requirement: 备份与文件权限

写入前，若目标文件已存在且内容将变化，SHALL 先备份为同目录的 `<file>.bak`。目标文件 SHALL 以 0600 写入；父目录缺失时 SHALL 递归创建。

#### Scenario: 覆盖前备份

- **WHEN** 目标 agent 配置文件已存在且需要更新条目
- **THEN** 先生成 `<file>.bak` 再写入

### Requirement: 明文落盘提示

导出计划 MUST 标注哪些条目会把明文值（`env`、`url`、`headers`）写入哪个文件。写入内容中的 `env`、`url` 与 header 值 SHALL 为导出时解析后的明文；任一引用无法解析时 SHALL 保留模板原文写入，MUST NOT 终止该 agent 的写入，并 SHALL 把每个无法解析的引用（含缺失的 env/text 条目名）逐条列入 warning 输出到 stderr。

#### Scenario: 计划标注明文

- **WHEN** 某 remote 档案含 headers 且目标 agent 将被写入
- **THEN** 计划中该条目标注会写入明文（含 headers）及其目标路径

#### Scenario: 引用解析失败

- **WHEN** 档案 `url` 引用 `{{env:secrets:MISSING}}` 且该 key 不存在
- **THEN** 该 agent 的目标文件仍被写入，`url` 值保留模板原文 `{{env:secrets:MISSING}}`，stderr 输出含 `env:secrets:MISSING` 的 warning，计划将该条目标注为含未解析引用

#### Scenario: 引用可解析时结果不变

- **WHEN** 档案所有引用均可解析
- **THEN** 导出结果与既有行为一致（解析为明文，无 warning）


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
