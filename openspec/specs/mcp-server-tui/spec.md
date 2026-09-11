# mcp-server-tui Specification

## Purpose
在 `senv tui` 中管理 MCP Server 档案并查看、执行对各 Coding Agent 全局配置的导出与撤回，使日常操作不必离开 TUI。
## Requirements

### Requirement: MCP Tab 注册

`senv tui` 在 vault 解锁后 SHALL 注册 MCP Tab，位置在 AI Tab 之后、History Tab 之前。`tui.Managers` 的 MCP 管理器为 nil 时 SHALL 跳过注册且不影响其他 Tab。

#### Scenario: 已解锁进入 TUI

- **WHEN** 用户解锁 vault 后启动 `senv tui` 且 MCP 管理器可用
- **THEN** Tab 栏出现 MCP Tab，可切入浏览

#### Scenario: 管理器未注入

- **WHEN** MCP 管理器为 nil 时启动 TUI
- **THEN** MCP Tab 不注册，TUI 正常启动无报错

### Requirement: 两栏布局与导出状态

MCP Tab SHALL 采用两栏布局：左栏为 MCP Server 档案列表（别名、command、env 键数），右栏为全部导出目标 Coding Agent（与 `senv mcp export` 支持的 agent 集合相同，含 claude-desktop 与 cursor）。右栏每行 SHALL 显示当前左栏档案的导出状态：未导出 / 已导出 / 漂移。`←→/hl` SHALL 在左右栏之间切换焦点并给出可见高亮，`↑↓` SHALL 只作用于当前焦点栏。列表行 SHALL 按截断规则显示，MUST NOT 因长值折行或撑高面板。`enter` SHALL 打开左栏当前档案的详情弹层。

#### Scenario: 浏览档案与导出状态

- **WHEN** 存在档案 github，且仅 cursor 的台账记录与内容一致
- **THEN** 左栏出现 github 行；右栏列出全部导出目标 agent；cursor 显示已导出，其余显示未导出

#### Scenario: 漂移展示

- **WHEN** 左栏选中 github，codex 上该条目台账指纹与文件内容不一致
- **THEN** 右栏 codex 行显示漂移

#### Scenario: 焦点切换生效

- **WHEN** 用户在 MCP Tab 按 `→/l` 再按 `↓/j`
- **THEN** 焦点移到右栏且高亮跟随，光标在 agent 列表中下移，左栏档案选择不变

#### Scenario: 无档案空态

- **WHEN** vault 中无任何 MCP Server 档案
- **THEN** Tab 正常渲染空态提示，说明可用 `n` 新建

### Requirement: 档案写操作

MCP Tab SHALL 提供档案写操作：`n` 新建、`e` 编辑选中档案、`d` 删除。新建表单 SHALL 收集别名、传输类型（`stdio` / `http` / `sse`）、description，并按传输收集对应字段：`stdio` 收集 command（必填）、args、env；`http` / `sse` 收集 url（必填）与 headers。选择 remote 传输时表单 MUST NOT 出现 command / args / env 字段。编辑表单 SHALL 使别名只读，MUST NOT 提供重命名键，且 SHALL 允许切换传输类型并按目标传输重新校验字段。args SHALL 以一行一个参数经 `$EDITOR` 编辑并保序；env SHALL 以一行 `KEY=VALUE` 经 `$EDITOR` 编辑，值按模板原样存储；headers SHALL 以一行 `Name: Value` 经 `$EDITOR` 编辑，值按模板原样存储。删除 SHALL 二次确认；若本机台账显示该档案已导出，确认文案 SHALL 列出相关 agent id，并说明删除不撤回已导出条目。写操作 SHALL 调用既有档案管理接口，成功后刷新左栏；失败 SHALL 经统一提示条回显且不改变既有档案。

#### Scenario: TUI 新建档案

- **WHEN** 用户按 `n` 填写别名 github、command `npx`、一行参数后提交
- **THEN** 档案保存成功，左栏出现 github，提示成功

#### Scenario: TUI 新建 remote 档案

- **WHEN** 用户按 `n` 选择传输 `http`、填写别名 web-reader 与 url 后提交
- **THEN** 档案保存成功，传输为 `http`，表单中未出现 command / args / env 字段

#### Scenario: 缺少 command 拒绝

- **WHEN** 用户提交新建表单但 command 为空
- **THEN** 表单内联报错并保持打开，不创建档案

#### Scenario: remote 缺少 url 拒绝

- **WHEN** 用户提交新建表单选择 `http` 但 url 为空
- **THEN** 表单内联报错并保持打开，不创建档案

#### Scenario: 编辑不改别名

- **WHEN** 用户对 github 按 `e` 修改 command 后提交
- **THEN** 档案更新，别名仍为 github

#### Scenario: 删除不撤回

- **WHEN** github 已导出到 cursor，用户按 `d` 并确认
- **THEN** vault 中档案被删除，cursor 全局配置中的已导出条目保持不变，确认文案曾列出 cursor

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

### Requirement: env 值可见性

MCP Tab 的列表、详情弹层、计划页与 toast MUST NOT 渲染 `env` 字面量、header 值或 `url` 的 query；列表与详情 MAY 展示 `env` 键名、引用模板原文与 `url` 的 `scheme://host` 来源。用户在档案表单中显式打开 `$EDITOR` 编辑 env 或 headers 字段时，编辑器是 TUI 内唯一允许出现对应字面量的解密面；主表单视图 MUST 只显示键名或键数量。

#### Scenario: 列表不泄漏值

- **WHEN** 档案 github 的 env 含字面量 token
- **THEN** 左栏与详情只出现键名，不出现该 token

#### Scenario: remote 详情不泄漏值

- **WHEN** 档案 web-reader 的 url 含 query 凭证且 headers 含 Bearer token
- **THEN** 左栏与详情只显示 `scheme://host` 来源与 header 键名，不出现 query 与 header 值

#### Scenario: 计划不渲染解析值

- **WHEN** 导出计划含将写入明文 env 或 headers 的条目
- **THEN** 计划页标注「明文」与目标路径，不显示解析后的值

#### Scenario: 编辑器是解密面

- **WHEN** 用户在编辑表单中打开 env 的 `$EDITOR`
- **THEN** 编辑器缓冲区含 `KEY=VALUE` 行；关闭编辑器回到表单后，表单主视图仍只显示键名
