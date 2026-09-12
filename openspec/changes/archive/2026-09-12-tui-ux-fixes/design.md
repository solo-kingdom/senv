# Design: tui-ux-fixes

## Context

`searchTab.View()` 渲染全部结果无窗口化（`search.go`）；`history_tab.go:221-226` 自带 `q → tea.Quit` 绕过顶层守卫；config All 伪组实现（`enterSidebarPlan` 以 `Scope{All:true}` 开计划）领先于 spec；tui-viewer Config Tab 需求写于侧栏改造（2026-09-01 tui-config-group-sidebar）之前。

## Goals / Non-Goals

**Goals:** 搜索结果窗口化；history 退出守卫统一；两处 spec 文本与实现/另一 spec 对齐。

**Non-Goals:** 按键变更、其它 Tab 迁移（见 driver design.md 拆分）。

## Decisions

- **窗口化复用 `windowedPane`**：searchTab 结果行改造为 `[]string` 交给既有窗口化原语，跟随 `listPageSize`；不新写滚动逻辑。备选「给 searchTab 单独分页」被否——两套窗口化并存正是本次要消除的形态。
- **退出守卫去重**：删除 history tab 内的 `q` 分支，退出只由 `model.go` 顶层处理；Tab 层不得再拦截 `q`（在 code review 清单中注明）。
- **All 伪组方向 = 改 spec 承认现状**（grill D6-3）：All 上整组 install/uninstall 是合理能力且实现已存在；备选「改实现遵守旧 spec」会砍掉已交付能力，被否。机制上旧需求「交互式安装与卸载」以 REMOVED（含 Reason/Migration）退役、ADDED「安装与卸载入口」接替——openspec 的 MODIFIED 块按 scenario 标题严格匹配、无法删除「All 伪组无整组操作」这一与新政相悖的场景，整块替换是唯一干净路径。

## 数据流与错误处理

均为视图层修复，不改数据装载路径；窗口化截断只影响渲染，搜索跳转（`searchJumpMsg`）行为不变；history 退出路径统一后，提示条错误优先级语义不变。

## Risks / Trade-offs

- [searchTab 结果行含类型徽标等宽字符，截断可能切坏对齐] → 复用 `truncateWidth` 按显示宽截断
- [spec 改动让「All 上误批量操作」少一道防线] → 计划预览 + 确认流仍在，误操作有确认兜底

## Open Questions

无。
