# tui-viewer 增量

## MODIFIED Requirements

### Requirement: Tab 内过滤

每个 Tab SHALL 支持按 `/` 键触发当前 Tab 内的过滤，仅匹配 key/name 等标识字段（不匹配值），匹配大小写不敏感。单栏 Tab 过滤作用于其主列表；SSH/AI/MCP 双栏 Tab 过滤作用于左栏主列表，右栏 SHALL 随左栏当前选中项联动；Config Tab 的过滤与侧栏计数行为见 config-tui 能力规约；Audit Tab 的预设过滤快捷键 SHALL 保留并与自由文本过滤叠加。`esc` SHALL 清除过滤并恢复完整列表。

#### Scenario: 过滤当前列表
- **WHEN** 用户在 Env Tab 按 `/` 键并输入 `DATABASE`
- **THEN** 右侧列表仅显示 key 含 `DATABASE` 的环境变量（忽略大小写）

#### Scenario: 双栏 Tab 过滤主列表
- **WHEN** 用户在 SSH Tab 按 `/` 键并输入 `web`
- **THEN** 左栏仅显示别名或 hostname 含 `web` 的 host（忽略大小写），右栏显示当前选中 host 的 keypair 联动信息

#### Scenario: 清除过滤
- **WHEN** 用户清空过滤输入框或按 `esc`
- **THEN** 列表恢复显示全部分组内的条目
