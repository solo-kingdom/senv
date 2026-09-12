# Design: tui-tab-consistency-render

## Context

- AI/MCP/SSH：`View()` 无 `!t.loaded` 分支，装载期落进 `len(x)==0` 空态分支（ai_tab.go `View`/`viewBaseAt`、mcp_tab.go 同构、ssh_tab.go 同构）；`loaded` 字段已存在，仅缺消费。装载为启动后台急加载，加载态主要为启动瞬间闪现，AI 侧因解析 models.dev 目录缓存窗口可观。
- history/audit：`View()` 末尾裸 `paneStyle.Render(...)` 不带 Width/Height；加载态直接 `return "加载…中…"`；`t.width` 存而不用；未走共享 `windowedPane`。`SetSize` 的 chrome 预算（history `height-4`、audit `height-6`）按旧结构估算。
- 共享原语齐备：`windowedPane`（窗口化标题+clip）、`stackWithOverlay`（overlay 压缩高度）、`clipLines`、`emptyStateStyle`；env 范式（env_tab.go `renderGroups`/`renderItems` 的 `!t.loaded` 分支）为对照实现。

## Goals / Non-Goals

**Goals:** 见 proposal「What Changes」；spec 见 `specs/tui-viewer/spec.md` 两条 ADDED 需求。

**Non-Goals:** 操作过程进度提示、双栏化、按键变更、数据装载路径与懒加载门控变更（History `visited` 语义、单趟快照语义不动）、AI/MCP/SSH `loadErr` 分支布局调整。

## Decisions

- **AI/MCP/SSH 加载守卫放 `viewBaseAt` 层而非 `View` 顶层**：`View` 顶层早退会让常驻几何失效（布局跳动、表单/向导 overlay 依赖 base 渲染）；在 base 渲染里对 `!t.loaded` 返回「标题行（paneTitleStyle，保留 Tab 名与过滤提示结构）+ 两栏各自 `emptyStateStyle.Render("加载…中…")`」再套既有 Width/Height 几何，与 env「框内嵌提示」同构。SSH 双栏（hosts/keypairs）同法。文案：AI/MCP/SSH 统一「加载档案中…」？——按对象区分：AI「加载 provider 中…」、MCP「加载 MCP 档案中…」、SSH「加载 SSH 资产中…」，对齐 env「加载分组中…」粒度。
- **history/audit 几何收口**：`View` 统一为「`windowedPane(标题, 行, cursor, 行预算, width)` + 底部附加行（history: detail/confirm/flash；audit: skipped/filter 提示）」→ `clipLines(总预算)` → `paneStyle.Width(t.width).Height(t.height)`；附加行存在时行预算按 `stackWithOverlay` 同法压缩（`lipgloss.Height` 计量）。`SetSize` 预算重算为「标题 1 行 + 附加行预留」随新结构核对。面板保持 `paneStyle`（不引入 `activePaneStyle`：单面板 Tab 无失焦形态，不扩语义）。
- **history 模式切换**：recent/entry 列表行已是 `[]string` 化渲染，接入 `windowedPane` 后删手写 `VisibleRange` 循环与区间提示缺失；detail/confirm/flash 沿用现有模式状态机，仅渲染位置收进面板。
- **spec delta 全 ADDED**（两条新需求落 `tui-viewer`），零 MODIFIED：既有「错误处理与空状态」「History Tab 延迟加载」「面板内容截断与详情」语义不受影响，无需改写。

## 数据流与错误处理

不改任何装载路径与消息协议（`*LoadedMsg` 广播语义不变）；history 装载失败仍走顶层错误条，audit `loadErr` 仍内嵌面板（补 Width/Height 后自然满足「错误态内嵌」）。加载态是纯渲染分支，不引入新消息、不触发额外查询。

## Risks / Trade-offs

- [受影响测试断言旧空态文案/旧几何] → 子 change 任务含逐 Tab 断言更新；以 `make check` 全绿为准
- [chrome 预算重算偏差致小终端溢出] → 复用 `stackWithOverlay`/`clipLines` 收口；补一条小终端渲染回归断言
- [与进行中的 keymap `Group` 重构同文件冲突] → apply 准备段核验工作树，dirty 且路径不在本 task change 内时停下确认

## Open Questions

无——grill frontier 已清空；加载文案粒度属实施细节，记录于此。
