## Why

三处缺陷与失配：全局搜索结果列表无窗口化，结果多时溢出外框（违反「内容区不溢出外框」既有要求）；history tab 自带 `q` 处理绕过顶层 dirty-quit 守卫；config 的 All 伪组 spec 禁止整组 install/uninstall 但实现已支持，tui-viewer 仍描述 Config Tab 为「单栏布局」与 config-tui 的双栏侧栏矛盾。

## What Changes

- `S` 全局搜索结果列表接入窗口化滚动，超出行截断，不再溢出
- history tab 退出统一走顶层 dirty-quit 守卫（有待推送条目先提示）
- spec 对齐（**BREAKING 仅 spec 层面，实现已就绪**）：config-tui「交互式安装与卸载」改为 All 伪组提供整组 install/uninstall（以全部条目为范围）；tui-viewer「Config Tab 浏览与操作」布局描述对齐双栏侧栏

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `config-tui`: 需求「交互式安装与卸载」整块退役，以「安装与卸载入口」接替——All 伪组整组操作由禁止改为提供（D6-3 改 spec 承认现状）
- `tui-viewer`: 「Config Tab 浏览与操作」单栏描述改为双栏分组侧栏，与 config-tui 规约一致

## Impact

- 代码：`internal/tui/search.go`（窗口化）、`internal/tui/history_tab.go`（退出守卫）
- 文档：`.agents/skills/senv-cli/SKILL.md`（如 TUI 描述涉及）

## Non-goals

- 不改任何按键语义（keymap 子 change 处理）
- 不动其它 Tab 的列表实现（list/filter 子 change 处理）

## 验证记录
- 2026-09-11（分支 tui-ux）：1.1 搜索结果窗口化落地（`visibleRange`/`listPageSize`/`truncateWidth`，预算=终端-外框5行-overlay4行）；1.2 `history_tab` 移除 `q→tea.Quit` 分支，退出统一走顶层 dirty-quit 守卫；1.3 核验通过：`enterSidebarPlan` All→`Scope{All:true}`→`PlanInstall/Uninstall` 全量计划，代码无禁令残留；2.1 SKILL.md 无 All/伪组描述，无需同步；2.2 `make check` 全部通过（internal/tui 125.6s 全绿）。
