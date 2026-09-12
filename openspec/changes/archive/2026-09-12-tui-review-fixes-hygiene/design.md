# Design: tui-review-fixes-hygiene

## Context

core 子 change 清掉全部 P1 与耦合 P2 后，剩余 P2 为独立内部缺陷：searchTab 自身在 tui-ux 窗口化改造后留下的五处缺口；List/窗格几何 helper 的边界条件；本地 helper 与 Go 1.21+ 内置函数的遮蔽；若干死代码与重复条件。证据见审查报告 `docs/reviews/2026-09-12/tui-tab-consistency/review.md`。

## Goals / Non-Goals

**Goals:** 逐条销账上述 P2（约 14 条），每条带回归用例或编译期证明（死代码）。

**Non-Goals:** 全部 P3（含 audit_tab 打磨项）；core 范围内的任何行为。

## Decisions

- **search.go 头部与宽度**：`Update` 增加 `tea.WindowSizeMsg` 分支（复用 model 层传入的 full terminal 尺寸换算，对齐注释声明）；头部在拼接 range 后缀后 `truncateWidth(header, innerW)`；结果行先按显示宽截断再套选中样式，避免 `truncateWidth` 误计 ANSI 转义宽度；选中行改用共享 `cursorLine`，删除本地分支；`{[]string{"type"}}` 声明删除（bubbletea 无此键名）
- **list.go 边界**：`paneBudgets` 在 minLeft clamp 后若 `left+5+4 > width` 则按比例收缩 left 保证 `right >= 4` 且总和 ≤ width；`Page` 在 `listPageSize <= 0` 时按 godoc 语义处理（无窗口 = 不窗口化，整列表一步）或修正 godoc——实施时以「行为最不意外」为准并同步注释；`SelectVisible` 空 keys 直接 return 并注释（保持现 no-op 行为但显式化）
- **helpers.go**：包级 `max(a, b int)` 改名 `maxInt`（调用点同批替换，全仓 `go build` 兜底）；删除 `maxLen`
- **config 表单校验**：create 表单 `group` 字段补 `validate` 回调，复用 `enterMetaMode` 的分组名校验（与 meta 编辑一致）
- **ai_tab 重复条件**：`1039-1040` 恢复为两个不同子表达式（原中/英双匹配意图，英文界面下保留对旧中文消息的兼容匹配或删旧分支——实施时确认消息源已全英文则删旧条件，二选一以编译后行为不变为准）
- **env 死代码**：直接删除，`go vet`/编译证明无引用

## 数据流与错误处理

全部为视图层内部修正；不新增消息、不改装载路径。search 头部截断只影响渲染。

## Risks / Trade-offs

- [`max` 改名触碰多文件] → 机械替换 + `make check` 兜底
- [search WindowSizeMsg 处理引入尺寸错位] → 用例覆盖 resize 后搜索窗口尺寸正确

## Open Questions

无。
