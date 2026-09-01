# Proposal: tui-config-group-sidebar

## Why

config 的分组能力在数据层与 CLI 已完备（`Create` 带组、`List(groupFilter)`、`Groups()`、`SetMeta`、install/uninstall 的 `--group/--all`），但 TUI config tab 仍是单列扁平列表——组只作为 `group/name` 前缀展示，无法按组聚焦浏览，整组 install/uninstall 的作用域也不直观。env/text tab 均为"左栏分组 + 右栏条目"的双栏形态，config tab 体验不一致。

## What Changes

- TUI config tab 重构为双栏布局：左侧分组栏（顶部 **All** 伪组 + 真实组列表，各组显示条目计数），右侧条目列表
- 默认选中 **All**，右侧显示全部配置（保留现有"一眼全览"体验）；选中真实组时右侧仅显示该组条目
- `←→/hl` 在分组栏与条目列表间切换焦点（与 env/text tab 一致）
- 整组操作锚定左栏：左栏选中真实组时 `I`/`U` 对该组执行 install/uninstall 计划预览；选中单条时 `i/I`/`u/U` 语义不变
- 全局搜索跳转 config 条目时同时选中对应分组与条目（`focusJump(group, name)`）
- `/` 过滤适配双栏：过滤条目列表，组计数随过滤结果更新

## Non-goals

- 不改动 config 数据层、存储格式与 CLI（分组能力已就绪）
- 不改动 env/text tab 的行为
- 不做组重命名、组删除等组管理操作（CLI 也未提供，留待后续）
- 不提取 env/text/config 三处重复的 sidebar 公共组件（保持 tab 独立演进，避免本变更扩散）

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `config-tui`: 分组浏览的呈现形态从单列扁平列表变为左侧分组选择栏 + 右侧条目列表，新增 All 伪组与双栏焦点导航；整组 install/uninstall 的作用域锚定左栏选中组

## Impact

- `internal/tui/config_tab.go`：主要重构（双栏渲染、焦点导航、组作用域操作、过滤）
- `internal/tui/model.go`：`applyJump` 的 config 分支传入 group
- `internal/tui/search.go`：jump 消息已携带 group，仅需接线
- 测试：`config_tab*_test.go`、search jump 相关测试更新
- 无数据层/CLI/存储格式影响，无安全面变化
