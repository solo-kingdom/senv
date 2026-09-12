# Grill：TUI tab 一致性（AI/MCP/SSH 加载态 + history/audit 几何）

2026-09-12 拷问收敛。事实核查基于 HEAD worktree 真实渲染探针（78×17 内容区）+ 代码交叉验证；当日工作区存在进行中的 keymap `Group` 重构（未提交、持续编辑），已确认该 diff 不触碰 View/加载逻辑，本结论对工作区同样成立。**本批排期在 keymap 重构落地之后。**

## 事实基线（Agent 核查，非决策）

- 8 个 Tab 三种加载行为：env/text/config 有加载提示且嵌在常驻面板几何内；AI/MCP/SSH 无 `!t.loaded` 分支，加载期显示带操作指引的空态文案（ai_tab.go `View`、mcp_tab.go `View`、ssh_tab.go `View`）；history/audit 加载态是裸文本、无面板。
- AI/MCP/SSH 为启动后台急加载（`Model.Init` 批量触发），本地读取，故症状是启动瞬间错误文案闪现；AI `Status()` 解析 models.dev 目录缓存，慢机器窗口可观。History 是唯一 visited 门控延迟加载（server 网络查询），其加载态最常驻。
- history/audit 渲染走裸 `paneStyle.Render` 不带 Width/Height：实测 History 装载后（1 行数据）面板 62×6、Audit 空态 24×5，其他 Tab 撑满 78×19；resize 时 `t.width` 已存但 View 不消费，边框不跟随、无 clip 可撑出溢出。
- history/audit 未走共享 `windowedPane`：窗口化标题（"4–12"）与防溢出 clip 均缺失。

## 决策记录

| # | 决策 | 结论 | 理由 | 状态 |
|---|------|------|------|------|
| D1 | 承接方式 | 新建 taskflow driver `tui-tab-consistency`，本文件沉淀结论，propose 拆 1 个子 change | 跨 8 个 Tab 的范式决策值得留档；改动小，单子 change 足够 | settled |
| D2 | 「加载提示」语义 | 只指**数据装载**层（加载态被空态顶替是主症状）；**操作过程**提示（MCP 导出/撤回、AI 切换、history 恢复进行中）移出本批，另立任务 | 与样式一致性同主题；操作多为本地毫秒级，history 恢复是唯一网络操作，值得单独立项不混批 | settled |
| D3 | 加载态范式 | env 范式推广：先画常驻面板几何，`!t.loaded` 时框内嵌「加载中…」；AI/MCP/SSH 双栏各自嵌；文案对齐「加载…中…」风格 | 彻底消除布局跳动，与 5 个既有 Tab 同构 | settled |
| D4 | history/audit 目标几何 | 单面板撑满 `contentW×contentH`，列表走共享 `windowedPane`，加载/空/错误三态内嵌面板，detail/confirm 嵌面板下部、超长 clipLines 截断；resize 跟随随之成立 | 这是一致性修复的本义；双栏化是重设计，不做（想要则单独立项） | settled |
| D5 | SSH 与范围边界 | SSH 纳入本批（与 AI/MCP 同根因，每处一行级）；不扩到 spinner 动画、骨架屏等美化 | 留着即是下次「不一致」的存量 | settled |

## 术语表

| 术语 | 本任务语境下的定义 | 与既有用词的关系 |
|------|--------------------|------------------|
| 加载态（Loading） | 数据请求后、返回前的过渡呈现；空态操作指引只允许装载完成后出现 | 新造（已入 CONTEXT.md「TUI 呈现」节）；修正了代码中 `loaded` flag 存在但 AI/MCP/SSH View 不消费的现状 |
| 空态（Empty） | 装载完成且数据集为空的稳定呈现，带操作指引 | 沿用既有「暂无 XX」文案语境，但与加载态显式切分 |
| 错误态（Error） | 装载/操作失败、展示失败原因的呈现；失败不是「没有数据」 | 沿用（error bar / `loadErr` 分支既有语义） |

## ADR 候选

- 无：本批决策均未同时满足「难逆转 + 后人费解 + 真实取舍」三门槛；结论以 grill.md 决策记录 + CONTEXT.md 术语承载。

## 未决问题

无。明确移出范围（非遗留）：操作过程进度提示（D2）、history/audit 双栏化（D4）、keymap `Group` 重构（外部进行中）。
