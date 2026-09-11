## Why

8 个 Tab 各自手写列表渲染与窗口滚动：audit 自维护 `top`/`pageSize`/`clampWindow`，history 自算 `visibleRows` 居中窗口，其余依赖 `windowedPane` 但游标/几何代码逐 Tab 重复。后续 filter（④）/多选（⑤）/侧栏（⑥）都要在列表状态上叠加，没有统一组件会继续复制。grill D1 已定共享组件收敛策略、D8 已定自研薄层。

## What Changes

- 新建共享列表组件：游标移动（含 `g`/`G`/PgUp/PgDn）、跟随式窗口化、行截断、双栏几何计算、按键经 keymap 注册表分发
- 收敛散落 helper：`paneBudget`/`visibleRange`/`clipLines`/`truncateWidth`/`padRunes`/`cursorPrefix` 及 `modalBox`/`cursorLine`/`orDash`/`sortedKeys`/`clamp`/`isPrintable` 提升为共享实现
- 迁移两个试点：audit tab（删手动窗口）、history tab（删自算窗口）；**零行为变化**——纯重构，滚动/翻页/截断外观与迁移前逐像素等价

## Capabilities

（无 spec 级行为变化——`.openspec.yaml` 已设 `skip_specs: true`）

## Impact

- 代码：`internal/tui/list.go`（扩展为组件）、`audit_tab.go`、`history_tab.go`、散落 helper 收敛
- 不改：数据装载路径、共享快照语义、任何 spec

## Non-goals

- 不迁移其余 6 个 Tab（它们随 ④⑤⑥ 的能力接入逐个迁移）
- 不在本 change 引入多选集与过滤状态（后续子 change 叠加）
- 不改任何按键语义（keymap ② 已完成）
