## Why
代码审查（`origin/main..HEAD`，2026-09-12）18 条 P1 + 若干耦合 P2：All 伪组视图下选择键、复制、解引用、行前缀用「All」字面量而非真实分组，`a` 全选静默失效、`y` 复制必败；filter 改造后 ai/mcp/ssh 多处游标仍按未过滤列表索引，过滤态下可操作到不可见条目；config `space` 键不分发导致多选入口失效；mcp/text 单选交错时 `e`/`d` 作用于游标项而非选择集；text 批量删除绕过操作审计且不刷新列表。

## What Changes
- All 伪组语义统一：选择键一律真实分组（env `a`、text `a`/`visibleKeys`）、All 视图行渲染 `group/key` 前缀（env/text）、`y` 复制与解引用改用条目真实分组（text/env）
- 过滤感知游标收敛：mcp `statusFor`/`clamp`、ai 可见集游标语义与过滤输入态透传、ssh `focusListLen`/`applyPendingJump`（jump 前清过滤契约）
- 多选与审计缺口：config `space` 兼容 `" "`、mcp/text 单选交错时 `e`/`d` 提示而非误操作、mcp `replanForce` 支持多选计划、text 批量删除逐条审计 + 完成后 reload、mcp 审计目标在多选计划下取真实别名、批量提交后统一清选择集、env 幽灵勾选与 `ok` 复用误激活
- config `enterSelectionPlan` 补 nil guard；config `selectedNames` 与选择集语义对齐

无 spec 增量：全部为实现对齐既有 spec（tui-viewer「多选集与批量操作」、operation-audit），见 driver design D2。

## Impact
- 代码：`internal/tui/{env,text,config,mcp,ssh,ai}_tab.go` 及对应 `_test.go`
- 审查依据：workspace `docs/reviews/2026-09-12/tui-tab-consistency/review.md`（core 销账清单见 design.md）

## Non-goals
- P3 全部不做；search/list/helpers 内部卫生归 `tui-review-fixes-hygiene`
- 不改键位语义表（config space 修复是对齐 D7 既有语义，非新键位）

## 验证记录
