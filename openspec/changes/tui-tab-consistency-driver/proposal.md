## Why
和 tab 页功能、样式不一致：1. AI MCP 没有加载提示；2. history & audit 边框宽、高和其他不一致（含 resize 不跟随；SSH 同根因一并纳入）。

grill 已收敛（见 `grill.md`）：根因是 AI/MCP/SSH 的 View 缺 `!t.loaded` 分支（加载态被空态文案顶替），以及 history/audit 渲染走裸 `paneStyle.Render` 不带 Width/Height（边框随内容伸缩、resize 不跟随、加载态无框）。

## What Changes
- 本 change 是 taskflow driver，不直接改代码，只编排子 change

## Non-goals
- 操作过程提示（MCP 导出/撤回、AI 切换、history 恢复进行中的进度指示）——grill D2 明确移出本批，另立任务
- history/audit 双栏化重设计——grill D4 明确不做，若想要单独立项
- spinner 动画、骨架屏等美化
- keymap `Group` 重构（工作区进行中的另一批工作，本批排在其落地之后）

## 涉及面
| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 会修改，实施前切任务分支 |

## 验收标准
- [x] AI/MCP/SSH Tab 数据装载期间显示加载态（env 范式：常驻面板几何 + 框内「加载中…」），空态文案仅在装载完成后出现
- [x] History/Audit 面板撑满内容区（contentW×contentH），resize 时跟随重排，与双栏 Tab 外框一致
- [x] History/Audit 列表走共享 `windowedPane`（窗口化标题 + 防溢出 clip），加载/空/错误三态内嵌面板
- [x] 新增/调整的渲染有测试覆盖（加载态、空态、几何、resize）
- [x] 全部子 change `validate --strict` 通过并归档

## Driver 协议
- 本 change 无 spec 增量（`.openspec.yaml` 已设 `skip_specs: true`）
- 子 change 一律命名 `{task}-<slice>`，与本 change 同一 planning root；跨 root 时在涉及面表显式记录 root 或 store id
- 实现进度只认子 change 自己的 `tasks.md`；本文件的 checkbox 只在对应子 change 全勾且 `validate --strict` 通过后才勾
- 涉及面里角色为 `必须` 的仓在实施前切任务分支：没有则 `git switch -c`，已有则 `git switch`。不许 stash / reset / 强制切换。工作树 dirty 时：未提交路径仅含当前 task 的 OpenSpec change（`openspec/changes/{task}-*`）则直接切；否则列出路径并确认是否继续 checkout。用户不同意、git 拒绝或切错仓时停下
- 只有「checkbox 全勾」「需要用户决策」「本轮预算耗尽」三种情况允许结束一轮；单项做不了就保持未勾，在验证记录写一行原因后继续下一项
- 结束时逐条列出未勾项与原因，不按 change 汇总

## 验证记录
- 2026-09-12（分支 tui-tab-consistency）：1.1 keymap 重构（24a7729）与 openspec 规划（888b733）分两笔提交后自 tui-ux 开切任务分支；2.1 子 change `tui-tab-consistency-render` 全部 checkbox 勾选且 `validate --strict` 通过；3.1 全仓 `make check`（fmt/vet/lint/test-race）全绿，internal/tui 131.3s。
- 未勾项：无。
