## MODIFIED Requirements

### Requirement: 导出与撤回

MCP Tab SHALL 提供导出与撤回：`x`/`u` 以左栏当前档案与右栏当前 agent 为范围，`X`/`U` 以左栏当前档案与全部导出目标 agent 为范围；范围 MUST 不依赖焦点在哪一栏。执行前 SHALL 展示计划页，逐条列出 agent、路径、动作（create / update / skip / drift / error）及是否标注「明文 env」；`enter`/`y` 确认后才写入，`esc`/`n` 取消且 MUST NOT 写盘。计划中的漂移与外部条目默认 skip；用户在计划页按 `F` 后 SHALL 将这些条目标为覆盖并刷新计划。撤回时，内容与 senv 期望一致的条目直接删除；被本地修改过的条目 SHALL 逐条 `y/n` 确认后才删除；逐条确认阶段（含帮助文案）`esc` SHALL 取消整个撤回操作——所有条目（含已回答 `y` 的）均不删除，并给出已取消提示；逐条确认阶段除 `y` 外的其余按键 MUST NOT 被解释为对整个操作的放行。计划中不存在任何需要写入的条目时，SHALL 提示无需写入且 MUST NOT 记录成功审计。导出与撤回 MUST 复用既有导出器（同一台账、同一 user 级全局配置、同一明文落盘语义）。

#### Scenario: 导出当前 agent

- **WHEN** 左栏选中 github、右栏选中 cursor，用户按 `x` 并在计划页确认
- **THEN** 仅 cursor 的全局配置被写入 github 条目，台账更新，右栏 cursor 显示已导出

#### Scenario: 导出全部 agent

- **WHEN** 左栏选中 github，用户按 `X` 并确认计划
- **THEN** 计划覆盖全部导出目标 agent，确认后逐个写入（单个失败不中止其余）

#### Scenario: 漂移默认跳过

- **WHEN** 计划中 cursor 条目标为 drift，用户直接确认且未按 `F`
- **THEN** cursor 文件不被修改，该条保持漂移

#### Scenario: 强制覆盖漂移

- **WHEN** 同样场景下用户在计划页按 `F` 后再确认
- **THEN** cursor 条目被覆盖，备份与台账更新

#### Scenario: 取消不写盘

- **WHEN** 用户打开导出计划后按 `esc`
- **THEN** 任何 agent 配置与台账均不变

#### Scenario: 撤回被改过需确认

- **WHEN** 用户按 `u` 撤回 github，目标条目已被本地修改
- **THEN** 计划确认后仍逐条询问，仅对回答 `y` 的条目删除

#### Scenario: 逐条确认 esc 取消整个撤回

- **WHEN** 用户在撤回的逐条确认中对第一条回答 `y`，在剩余条目未回答完之前按 `esc`
- **THEN** 整个撤回被取消：已回答 `y` 的条目也不删除，任何 agent 配置与台账不变，界面提示已取消，帮助文案与实际行为一致

#### Scenario: 撤回无待写入条目时不虚报成功

- **WHEN** 用户撤回的档案从未导出（计划仅含 absent 条目）并确认
- **THEN** 界面提示无需写入，不显示「已撤回」，审计不记录成功的撤回事件

#### Scenario: 无档案时导出被拦截

- **WHEN** 左栏无选中档案，用户按 `x`
- **THEN** 提示没有可导出的档案，不打开计划页、不写盘
