# mcp-server-export Delta

## MODIFIED Requirements

### Requirement: 导出目标选择

`senv mcp export` MUST 显式指定目标：`--agent <id>[,<id>...]` 或 `--all`。未指定目标时 SHALL 报错并列出支持的 agent id；指定未知 agent 时 SHALL 报错且不产生任何写入。导出目标 SHALL 为 agent 的配置文件，由 `--scope user|project` 决定（默认 `user`）：`user` 为 agent 的全局配置文件；`project` 为项目级配置文件，且仅部分 agent 生效（如 cursor 写 CWD 相对的 `.cursor/mcp.json`），不区分 scope 的 agent 两 scope 解析到同一路径。TUI MCP Tab SHALL 提供 scope 切换键（默认 `user`，会话内有效），右栏导出状态列、`x/X` 导出计划均按当前 scope 计算，界面 SHALL 常显当前 scope；scope 选择 MUST NOT 改变档案解析与写入语义（与 CLI 同一 `Exporter`）。

#### Scenario: 未指定目标报错

- **WHEN** 执行 `senv mcp export` 且未给 `--agent` 或 `--all`
- **THEN** 报错并列出支持的 agent id，不写任何文件

#### Scenario: 未知 agent 报错

- **WHEN** 执行 `senv mcp export --agent nosuchagent`
- **THEN** 报错，且所有目标文件保持不变

#### Scenario: project scope 仅对部分 agent 生效

- **WHEN** 执行 `senv mcp export --all --scope project`
- **THEN** cursor 的目标路径为项目级 `.cursor/mcp.json`，其余不区分 scope 的 agent 目标路径与 `user` scope 相同，计划逐条列出实际路径

#### Scenario: TUI 切换 scope

- **WHEN** 用户在 MCP Tab 按 scope 切换键
- **THEN** 当前 scope 在 `user` 与 `project` 间切换，右栏标题显示新 scope，导出状态列立即按新 scope 重算，无副作用发生

#### Scenario: TUI 默认 user scope

- **WHEN** 用户打开 MCP Tab 且未切换过 scope
- **THEN** 状态列与导出计划与既有行为一致（`user` scope），不产生任何 project 级写入

### Requirement: 撤回导出

`senv mcp unexport` SHALL 删除目标 agent 配置中由 senv 导出的条目：条目内容与 senv 期望一致时 SHALL 直接删除；被本地修改过时 SHALL 要求确认后才删除。删除档案 MUST NOT 自动触发撤回。撤回的目标文件由 `--scope`（CLI）或 MCP Tab 当前 scope（TUI）决定，TUI 的 `u/U` 撤回 SHALL 与导出使用同一 scope。

#### Scenario: 内容一致直接删除

- **WHEN** 目标条目与 senv 期望内容一致时执行 `senv mcp unexport`
- **THEN** 该条目被删除，配置中其它内容保留

#### Scenario: 被改过需确认

- **WHEN** 目标条目被本地修改过时执行 `senv mcp unexport`
- **THEN** 提示该条已被修改，确认后才删除

#### Scenario: TUI 撤回按当前 scope

- **WHEN** MCP Tab 当前 scope 为 `project` 时按 `u`/`U`
- **THEN** 撤回计划只针对 project 路径下台账记录的条目；在 project 文件中不存在的条目标注 `absent` 且不被修改
