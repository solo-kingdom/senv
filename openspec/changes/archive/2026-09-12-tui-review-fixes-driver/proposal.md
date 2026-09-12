## Why
代码审查（2026-09-12，范围 `origin/main..HEAD`，报告见 workspace `docs/reviews/2026-09-12/tui-tab-consistency/`）发现 P0=0、P1=18、P2=24、P3=27。P1 集中在三条主线：① All 伪组下选择键/复制/解引用/前缀渲染用「All」字面量而非真实分组，选择集与批量动作错位；② 过滤感知游标改造不彻底（ai/mcp/ssh 多处仍用未过滤列表索引或游标语义混用）；③ 多选与审计缺口（config space 键不分发、单选交错时 e/d 误操作游标项、text 批量删除绕过审计且不刷新）。P2 为同主题次要缺陷与内部卫生问题。

## What Changes
- 本 change 是 taskflow driver，不直接改代码，只编排子 change

## Non-goals
- P3×27 全部不做（打磨项：命名、注释、微优化、Makefile 加固等），后续如需要另立 change
- 不改键位语义表与 keymap 注册表结构（修复对齐既有 D7 语义表与 spec）
- 不动 history/audit 几何与加载态（tui-tab-consistency-render 已交付）

## 涉及面
| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 会修改，实施前切任务分支 |

## 验收标准
- [x] 18 条 P1 全部修复且各有回归测试（All 伪组语义、过滤感知游标、多选与审计缺口）
- [x] 24 条 P2 全部修复（含 search.go 窗口化卫生、list/helpers 内部缺陷、死代码清理）
- [x] `make check`（fmt/vet/lint/test-race）全绿
- [ ] 两个子 change `validate --strict` 通过并归档

## Driver 协议
- 本 change 无 spec 增量（`.openspec.yaml` 已设 `skip_specs: true`）
- 子 change 一律命名 `{task}-<slice>`，与本 change 同一 planning root；跨 root 时在涉及面表显式记录 root 或 store id
- 实现进度只认子 change 自己的 `tasks.md`；本文件的 checkbox 只在对应子 change 全勾且 `validate --strict` 通过后才勾
- 涉及面里角色为 `必须` 的仓在实施前切任务分支：没有则 `git switch -c`，已有则 `git switch`。不许 stash / reset / 强制切换。工作树 dirty 时：未提交路径仅含当前 task 的 OpenSpec change（`openspec/changes/{task}-*`）则直接切；否则列出路径并确认是否继续 checkout。用户不同意、git 拒绝或切错仓时停下
- 只有「checkbox 全勾」「需要用户决策」「本轮预算耗尽」三种情况允许结束一轮；单项做不了就保持未勾，在验证记录写一行原因后继续下一项
- 结束时逐条列出未勾项与原因，不按 change 汇总

## 验证记录
- 2026-09-12（分支 tui-review-fixes）：1.1 自 tui-tab-consistency（干净）开切；2.1/2.2 子 change core、hygiene 全部 checkbox 勾选且 `validate --strict` 通过；3.1 全仓 `make check`（fmt/vet/lint/test-race）全绿。core 新增 17 个回归用例（review_fixes_core_test.go），hygiene 新增 7 个（review_fixes_hygiene_test.go）；ai 重复条件与 env 死代码为编译期证明。
- 未勾项：无。
