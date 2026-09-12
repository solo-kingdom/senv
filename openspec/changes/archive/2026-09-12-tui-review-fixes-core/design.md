# Design: tui-review-fixes-core

## Context

findings 根因与证据见审查报告 `docs/reviews/2026-09-12/tui-tab-consistency/review.md`（P1/P2 各条含代码摘录）。根因三类：① tui-ux 多选/过滤落地时六 Tab 消费点未同步切换语义；② 键名分发遗漏（bubbletea 空格键 `msg.String()` 为 `" "`）；③ text 批量路径复制单条路径时丢审计/刷新。抽查已确认 4 条关键 P1 属实（config space、text All 复制、env All 选择键、mcp statusFor）。

## Goals / Non-Goals

**Goals:** 本 change 销账审查 P1×18 全部 + 以下耦合 P2：env `resolveDeref`/幽灵勾选、text `visibleKeys`/前缀/批量导出校验、mcp 审计目标 ×2/`ClearSelection`/`/` 焦点/`applyPendingJump` 清过滤、config `selectedNames`/选择集清理。

**Non-Goals:** P3；search/list/helpers 卫生（归 hygiene）；键位语义变更。

## Decisions

### D1 选择键唯一事实源（销账 env:390、text:345/1082/1096、env:1144）

- `a` 全选与 `visibleKeys` 一律 `it.group+"/"+it.key`（等价 `selectionKey()` 语义），All 伪组仅是视图聚合，字面量 `"All"` 不得进入选择标识
- All 视图行渲染前缀条件改为 `group == envAllLabel`（env）/等效真实分组判断（text），聚合行显示 `group/key`
- `y` 复制（text `doCopy`）与解引用（env `resolveDeref`）改用条目真实分组 `it.group`

### D2 游标唯一事实源 = 过滤可见列表（销账 ai:586、mcp:206/562、ssh:426/1264/1271）

- mcp `statusFor` 接收可见列表或按 `visibleServers()` 定位选中项；`clamp()` 对 `len(visibleServers())` 收口
- ssh `focusListLen` 返回过滤后长度；`applyPendingJump` 按契约先 `filterBox.Clear()`（迁移 tui-ux-filter「jump 前清过滤保证目标可见」），再在可见列表上定位
- ai 左栏游标全面切换可见集语义：`clampLeft`/`currentProvider`/`providerLines`/`jumpFocus`/`focusListLen` 同批核对
- 过滤输入态（ai:353）：输入态吞掉导航/实体键（对齐 env 过滤分支范式），仅放行 esc/enter/backspace/可打印字符

### D3 多选确认面统一（销账 config:481/856/892/1011、mcp:395/422/934/340/990/1022、text:353/636/646/653、env:385/426）

- config space 分发改 `case " ", "space":`（env 已是此写法）
- 单选交错（选择数 ==1 且游标不在选择上）：`e`/`d` 提示「缩小到单选」而非静默操作游标项——与选择数 ≥2 的既有提示同文案族
- text `doBatchDelete`：逐条 `recordAudit`（成功与失败，对齐 operation-audit），完成后返回 `textReloadMsg`（对齐单删）；`doBatchExport` 目标文件名经 `securefs.ValidateSegment` 复验
- mcp `replanForce` 以计划条目集为准而非 `pendingAlias`；审计目标多选计划用 `mcpExportAuditTarget(plan)` 族 helper（与成功路径一致）；`ClearSelection` 提交后统一（含 `doDelete`/表单成功/外部 reload 后 reconcile）
- env `case "a"` 幽灵勾选：`currentItem()` 失败时不写入空键；`doActivate` 双 `ok` 检查独立判断，任一失败早退
- config `enterSelectionPlan` 闭包补 `mgr == nil` guard（对齐同文件其他路径）

## 数据流与错误处理

不改装载路径与消息协议；选择集/游标是纯视图状态。批量审计走既有 best-effort writer（失败仅告警不阻断）。所有修复不新增消息类型，reload 复用既有 `*ReloadMsg`/`textReloadMsg`。

## Risks / Trade-offs

- [六 Tab 语义切换回归面大] → 每条 finding 至少一个回归用例，含过滤态与 All 视图组合场景；`make check` 全绿为门槛
- [可见集语义切换可能暴露既有隐藏耦合] → 实施时以审查报告逐条核对消费点，发现报告遗漏的同类问题按同范式顺手修并记录
- [单选交错行为变化（提示替代静默操作）] → 与选择数 ≥2 既有提示一致，属 spec「单实体操作受限」的边界补全

## Open Questions

无。
