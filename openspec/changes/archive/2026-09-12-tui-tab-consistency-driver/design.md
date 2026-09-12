# Design: tui-tab-consistency-driver

## Context

现状与事实基线见 `grill.md`（5 项 settled 决策）；动机见 proposal「Why」。代码事实：8 个 Tab 三种加载行为，AI/MCP/SSH 的 View 缺 `!t.loaded` 分支（加载态被带操作指引的空态顶替）；history/audit 渲染走裸 `paneStyle.Render` 不带 Width/Height（面板随内容伸缩、resize 不跟随、加载态无框、未接入共享 `windowedPane`）。本 driver 不直接改代码，只编排 1 个子 change；设计决策已由 grill 收敛，本文记录拆分与 spec delta 策略。

## Goals / Non-Goals

**Goals:**

- AI/MCP/SSH 补齐加载态：env 范式（常驻面板几何 + 框内「加载中…」），空态文案仅在装载完成后出现（D3）
- history/audit 面板撑满内容区并接入共享 `windowedPane`，resize 跟随（D4）
- SSH 同根因一并纳入（D5）
- spec 层固化「加载态/空态/错误态」切分与面板几何要求（CONTEXT.md「TUI 呈现」术语已落地）

**Non-Goals:**

- 操作过程进度提示（MCP 导出/撤回、AI 切换、history 恢复进行中）——D2 明确移出，另立任务
- history/audit 双栏化重设计、spinner/骨架屏美化
- 按键语义变更（keymap `Group` 重构为外部进行中工作，本批排其之后）

## Decisions

### D1 单子 change 拆分：`tui-tab-consistency-render`

两类修复（加载态范式、面板几何）同属 View 渲染层、共享同一测试基建（headless `tea.Model` 渲染断言），拆两个子 change 会人为制造先后依赖与重复回归；单子 change 一次 `make check` 覆盖。

### D2 spec delta 全部走 tui-viewer ADDED，不动 llm-provider-tui / mcp-server-tui / ssh-assets

- 加载态是**跨 Tab 范式**而非单 Tab 行为，落 `tui-viewer`（TUI 全局能力位）新增「Tab 加载态」需求，scenario 显式点名 AI/MCP/SSH——三份 Tab 能力 spec 各写一份 MODIFIED 会复制同一规则三遍，正是本批要消除的漂移形态
- history/audit 几何落 `tui-viewer` 新增「History 与 Audit 面板几何」需求；既有「History Tab 延迟加载」「错误处理与空状态」「面板内容截断与详情」语义均不变、无冲突，零 MODIFIED
- 符合 driver 惯例「能新增（ADDED）不修改（MODIFIED）」

### D3 实现要点（子 change design 展开）

- AI/MCP/SSH：在 `viewBaseAt` 层做 `!t.loaded` 守卫，两栏各自嵌 `emptyStateStyle` 加载文本，面板几何照常渲染（布局零跳动）；`loadErr` 分支维持现状
- history/audit：`View` 统一为「`windowedPane`(标题+列表行) + 底部附加行（detail/confirm/flash/filter 提示）」后 `clipLines` 收口，外层 `paneStyle.Width(t.width).Height(t.height)`；`SetSize` 的 chrome 预算随新结构重算

## Risks / Trade-offs

- [现有测试断言旧行为（如 AI/MCP 加载期显示空态文案）] → 子 change tasks 含「更新受影响断言」条目，`make check` 为准
- [history/audit chrome 预算重算引入高度偏差] → 沿用 `stackWithOverlay`/`clipLines` 既有收口原语，冒烟覆盖小终端
- [排期依赖 keymap 重构落地] → apply 准备段检查工作树，冲突时停下确认

## Migration Plan

无存储格式变更、无 CLI 接口变更。纯渲染层修复，单子 change 独立可回退。

## Open Questions

无——grill frontier 已清空。
