## Why

多选目前只存在于 AI 切换向导；config/MCP 的批量操作靠 scope 快捷键（整组/全部），无法圈定任意子集（如「All 视图里挑三个不同组的条目一起装」）。grill D2/D9 已定范围与语义。

## What Changes

- 共享列表组件增加多选集：`space` 勾选/取消、`a` 全选当前过滤可见集（再按取消全选可见集）；选择集跨过滤持久，状态栏提示已选数与被过滤隐藏数
- 批量安全动词复用单项键、作用于选择集：env/text/ssh 批量 `d`/`x`，config 批量 `i`/`u`（跨组合并计划），MCP 批量 `x`/`u`/`X`/`U`（选择集 × agent 范围）
- 空选择集回落游标单条（纯单选行为不变）；单实体操作（`e`/`r`/`m`/详情）仅选择数 ≤1 可用；选择不跨栏；批量提交后清空选择集
- 全部批量写走既有确认流（计划预览/逐条确认/删除二次确认），单条失败不中止其余

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `tui-viewer`: ADDED「多选集与批量操作」——跨 Tab 的选择集语义总纲
- `config-tui`: ADDED「多选批量安装与卸载」——items 窗选择集与 `i`/`u` 批量计划
- `mcp-server-tui`: ADDED「多选批量导出与撤回」——档案选择集与 `x`/`u`/`X`/`U` 范围扩展

## Impact

- 代码：`internal/tui/list.go`（选择集状态）、`env_tab.go`、`text_tab.go`、`ssh_tab.go`、`config_tab.go`、`mcp_tab.go`、plan 流程批量化
- 文档：`.agents/skills/senv-cli/SKILL.md`

## Non-goals

- AI 向导多选保持自有实现不动（grill D2）；不加 visual mode；scope 快捷键 `I`/`U`/`X`/`U` 保留共存，不退役

## 验证记录
- 2026-09-11（分支 tui-ux）：`List` 组件增加多选集（Toggle/SelectVisible/IsSelected/SelectionCount/SelectedIn/ClearSelection/SelectionHint），以稳定标识为键；env/text/ssh/config/mcp 五类列表接入 `space`（bubbletea 空格键为 `" "`）与 `a` 全选可见集；批量动词：env/text/ssh `d`（一次确认列全部目标，逐条删除单条失败不中止）、text `x`（目录表单，逐块写 `<dir>/<key>.txt`）、ssh `x`（目录表单，逐 host 写 `<dir>/<alias>.conf`，沿用 WriteSensitiveFile 0700/0600）、config `i`/`u`（按选择集逐条 Plan 合并为一份计划，changed 逐条确认沿用）、MCP `x`/`u`/`X`/`U`（exporter.Plan/PlanUnexport 直接收别名列表，范围=选择集×agent(s)）；空集回落游标单条；`e`/`r`/`m` 多选时提示需单选；提交即清空选择集；面板标题提示「已选 N（M 被过滤）」；新增 4 个多选测试（env 批量删除真删、过滤全选与持久、单实体限制、MCP 批量导出计划）；SKILL.md 补多选段；`make check` 全部通过。
