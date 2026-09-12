# Design: tui-review-fixes-driver

## Context

审查发现全部来自同一分支演进过程：tui-ux 系列先落地了「未过滤列表」语义的游标与选择集，随后 filter/multiselect 改造切换到「过滤可见集」语义，但 env/text/config/mcp/ssh/ai 六个 Tab 共 20+ 处消费点没有同步切换（选择键用 `currentGroup()+"/"+key`、游标用 `t.servers[t.serverIndex]`、`focusListLen` 忽略过滤等）；config 的 space 键分发漏了 bubbletea 的 `" "` 键名；text 批量删除复制单删路径时丢了审计与刷新。完整 findings 清单（含 file:line、证据与修复建议）见 workspace `docs/reviews/2026-09-12/tui-tab-consistency/review.md`。

本 driver 不直接改代码，只编排 2 个子 change；修复均为「实现对齐既有 spec」（tui-viewer「多选集与批量操作」、operation-audit「TUI 写操作与 CLI 同类事件」），无 spec 增量。

## Goals / Non-Goals

**Goals:**

- P1×18 全部修复：All 伪组语义统一（选择键/复制/解引用/渲染前缀）、过滤感知游标收敛（ai/mcp/ssh）、多选与审计缺口（config space、单选交错、批量审计与刷新）
- P2×24 全部修复：同主题耦合项随 core 处理；search.go 窗口化卫生、list/helpers 内部缺陷、死代码等归 hygiene
- 每条修复有对应回归测试

**Non-Goals:**

- P3×27 不做；不改键位语义、不动已交付的加载态/几何

## Decisions

### D1 拆两个子 change，按「语义修复 vs 内部卫生」切分

- `tui-review-fixes-core`：全部 P1 + 与同一代码路径紧耦合的 P2（All 伪组语义、过滤游标、多选确认面、审计缺口、nil guard）——一次改到位避免同一函数两拨人动
- `tui-review-fixes-hygiene`：其余 P2（search.go 五项、list.go 三项、helpers 两项、config 表单校验、ai 重复条件、env 死代码 ×3）——不改变外部行为，独立可回退
- mcp_tab.go 同时出现在两批：core 先行、hygiene 跟后，串行实施避免冲突

### D2 全部子 change `skip_specs: true`

抽查确认所有修复均为「实现对齐既有 spec」：多选语义以 tui-viewer「多选集与批量操作」为准（space 勾选光标条目、a 全选可见集、批量作用选择集、单实体 ≤1 限制），审计以 operation-audit「TUI 写操作 MUST 与 CLI 同类事件」为准。无新增外部行为，零 spec delta。

### D3 修复范式约定（core 内统一，避免六 Tab 各修各的）

- 选择键唯一事实源：一律 `selectionKey()`/`it.group+"/"+key`（真实分组），All 伪组仅是视图聚合，不得进入任何标识符
- 游标唯一事实源：双栏 Tab 游标一律相对「过滤可见列表」；所有下标消费点（statusFor/clamp/focusListLen/applyPendingJump）同批切换，禁止再读未过滤列表长度
- 过滤输入态：输入态下非过滤键 MUST NOT 透传到列表导航/实体动作（对齐 env 范式）
- 批量动作与单条动作共用同一收尾：审计逐条、失败汇总、提交后清选择集并 reload

## Risks / Trade-offs

- [改动面横跨 6 个 Tab、43 条 findings] → core/hygiene 分批串行；每子 change `make check` + 逐条回归用例，审查报告作为核对清单逐条销账
- [过滤感知游标重构可能引入新错位] → 以「可见列表长度」断言写进测试；沿用 tui-ux-list 的 `List` 组件既有 clamp 语义
- [与后续功能开发并发] → driver 分支独立，实施前按协议核验工作树

## Migration Plan

无存储、无 CLI 接口变更。两个子 change 独立可回退；core 先行（P1 全清），hygiene 可独立推迟不阻塞。

## Open Questions

无——scope（P1+P2 做、P3 不做）已按审查结论与用户「制定修复提案」指令定为推荐解；若要调整 P3 范围，在 apply 前提出即可。
