## Context

`Manager.Apply`（ssh-host-export-apply 落地）已是 CLI 应用导出的唯一编排：写组片段、落盘缺失私钥、注册 Include、全量清理幽灵片段，返回结构化 `ApplyResult`（Written/Pruned/Materialized/KeysSkipped/Registered/Warnings/Errors）。TUI 侧现状：`x` 纯渲染预览 + `w` 手填目标文件；批量 `x` 手填目录（placeholder 残留旧教学 `~/.ssh/config.d`，且填进 `~/.ssh/senv/` 内会被幽灵清理误删——footgun）。

## Goals / Non-Goals

**Goals:**
- TUI 内完成与 CLI 等价的应用导出，位置自动推断，不再手填 target
- 「按组选择」复用既有分组侧栏：host 栏 `a` = 所在组、侧栏 `a` = 选中组（All = 全量），零新选择 UI
- 批量导出目录表单加 footgun 防护

**Non-Goals:**
- 持久化导出状态台账、TUI 的 unexport/prune 入口（proposal 已列）
- 改动 `internal/ssh` 任何行为（纯消费方）

## Decisions

### D1 按键与作用域

host 栏 `A` → `Apply({Host: cursorAlias})`（写入单元是该 host 所在组整文件，与 CLI 一致）；侧栏 `A` → 选中「All」时 `Apply({})`，否则 `Apply({Group: 选中组})`。keyactions 在两栏的条目操作组中注册 `A apply`，help 面板同步。

- 键位勘误（能力矩阵审计发现）：`a` 已被 host 栏多选全选占用（`ssh_tab.go:445`），apply 用大写 `A`——`G`（jump bottom）已有大写先例。
- 备选：预览框内加 `A`——多一步且预览是纯渲染语义，混入 apply 易混淆；独立按键意图更直。
- 备选：新建组选择弹层——与侧栏能力重复，违背最小交互面。

### D2 确认框即状态展示

确认 modal 在按键后、执行前实时计算并列出：重建组片段数（`len(Written)` 需先 Render 预算——用 `Render` 预算组数与 warning，用 vault 引用集预算待落盘数）、Include 已注册/待注册、warning 计数。`enter/y` 执行、`esc/n` 取消。

- 预算实现：`mgr.Render(filter)` 得 `Order`/`Warnings`/`Referenced`；待落盘数 = Referenced 中目标路径不存在的数量（Lstat 检查，与 Apply 判定同规则）。
- 不做持久化状态：确认框 + toast 已回答「将发生什么/发生了什么」；导出状态台账是另一个产品决定。

### D3 执行与结果呈现

执行走 `tea.Cmd` 异步（同 `doBatchExportHosts` 模式），结果 toast：`applied: N groups, M keys materialized, K skipped, include registered/existed, W warnings`。`Apply` 返回 error 时（部分失败汇总）toast 红色并附首条错误。warning 明细不在 TUI 渲染（沿用 TUI 不展开 warning 明细的惯例，计数指路 CLI）。

### D4 批量与单条导出目标防护

批量目录表单与单条导出（预览后 `w`）的目标文件表单，`validate` 统一校验：展开 `~` 后若落在 `~/.ssh/senv` 之内（含自身）→ 报错「该目录树由 senv apply 维护，外来文件会被清理；请用 `A` 应用导出或另选用户自有路径」。placeholder 由 `~/.ssh/config.d`（批量）/`~/.ssh/config.d/senv`（单条）改为中性用户目录。纯前端校验，不挡 CLI `--output`（CLI 纯渲染允许任意路径，是用户显式意图）。

## 错误处理策略

- `Render` 失败（proxyJump 悬空、撞 `_ungrouped`、组不存在）：按键即弹错误 toast，不进确认框（零副作用保证与 CLI 一致）。
- `Apply` 部分失败：toast 红色 + 首条错误；已成功项不被回滚（与 CLI 语义一致）。
- 确认框取消：无副作用。
- 异步 Cmd 期间界面可继续操作，结果以 msg 返回刷新（既有模式）。

## Risks / Trade-offs

- 确认框预算与实际执行之间 vault 被并发改动（极低概率，单用户 CLI）→ 接受；toast 呈现实际结果为准。
- 又一个按键（`a`）挤占 SSH Tab 键位 → `a` 尚未占用且语义 mnemonic；help 面板列出不增加学习成本。
- TUI 不展示 warning 明细 → 计数指路 CLI，保持 TUI 渲染简洁的既有取向。

## Migration Plan

无数据迁移；SKILL.md TUI 按键说明与 README TUI 一节同步更新；发布说明可并入下一次发版。

## Open Questions

无。
