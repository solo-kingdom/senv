## Why
提升 TUI 的用户体验：统一支持分组、多选、搜索等交互能力，并补充其他易用性改造。

现状（代码事实，2026-09-11 盘点）：
- 8 个 Tab（Env/Text/Config/SSH/AI/MCP/History/Audit）各自手写列表渲染与按键处理；共享面仅有 `windowedPane`、styles、form、detail overlay。
- 多选只在 AI 切换向导（`flowSelected`）存在；过滤（`/`）只在 env/text/config/audit 存在且逻辑复制 4 份；分组只在 env/text/config 存在，config 是唯一的侧栏模式。
- 按键语义跨 Tab 冲突：`r`（rename/refresh/restore 四义）、`e`、`x`、`u`、`m`、`i` 各有多义；`g`/`G`、PgUp/PgDn 仅部分 Tab 支持；`esc` 语义不一。
- help 文案字符串是 de-facto keymap 真相源（`help.go` 解析 `Help()` 字符串）。

## What Changes
- 本 change 是 taskflow driver，不直接改代码，只编排子 change

## Non-goals
- 不改变 TUI 的功能范围（不新增业务能力，只统一与增强交互）
- 不替换 bubbletea/lipgloss 技术栈
- 不在本 driver 内决定具体拆分粒度（留给 propose）

## 涉及面
| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 会修改，实施前切任务分支 |

## 验收标准
- [ ] 7 个子 change（fixes/keymap/list/filter/multiselect/sidebar/forms）全部 apply 完成：各自 tasks.md 全勾且 `openspec validate --strict` 通过
- [ ] 按键语义符合 grill.md D7 附录表：`r`=rename 唯一、refresh=`Ctrl+R`、env 组 `t`、text 导出 `x`、AI model-only `M`、确认框统一、help overlay 与实际键位一致
- [ ] 多选/过滤/侧栏能力按 D2/D3/D4 范围可用：五类列表多选、`/` 过滤全 Tab、env/text 侧栏（All 伪组+计数）
- [ ] 修复项落地：`S` 搜索结果窗口化、history `q` 过 dirty-quit 守卫、config All 伪组与 Config Tab 布局 spec 对齐
- [ ] `.agents/skills/senv-cli/SKILL.md` TUI 键位小节与新交互同步；`make check` 通过

## Driver 协议
- 本 change 无 spec 增量（`.openspec.yaml` 已设 `skip_specs: true`）
- 子 change 一律命名 `tui-ux-<slice>`，与本 change 同一 planning root；跨 root 时在涉及面表显式记录 root 或 store id
- 实现进度只认子 change 自己的 `tasks.md`；本文件的 checkbox 只在对应子 change 全勾且 `validate --strict` 通过后才勾
- 涉及面里角色为 `必须` 的仓在实施前切任务分支：没有则 `git switch -c`，已有则 `git switch`。不许 stash / reset / 强制切换。工作树 dirty 时：未提交路径仅含当前 task 的 OpenSpec change（`openspec/changes/tui-ux-*`）则直接切；否则列出路径并确认是否继续 checkout。用户不同意、git 拒绝或切错仓时停下
- 只有「checkbox 全勾」「需要用户决策」「本轮预算耗尽」三种情况允许结束一轮；单项做不了就保持未勾，在验证记录写一行原因后继续下一项
- 结束时逐条列出未勾项与原因，不按 change 汇总

## 验证记录
