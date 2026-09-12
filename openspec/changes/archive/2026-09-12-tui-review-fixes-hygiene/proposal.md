## Why
代码审查（`origin/main..HEAD`，2026-09-12）其余 P2：search.go 窗口化自身五处缺陷（不处理 WindowSizeMsg、头部超宽、ANSI 宽度误计、选中行渲染偏离共享 helper、不可达键位声明）、list.go 内部缺陷（paneBudgets 小宽度溢出、Page 退化语义、SelectVisible 空集 no-op）、helpers 遮蔽内置 `max` 与 no-op 包装、config 新建表单 group 字段缺校验、ai 重复条件、env 三处死代码。

## What Changes
- search.go：`Update` 处理 `tea.WindowSizeMsg`；头部截断到 `innerW`；styled 行避免进 `truncateWidth`（先截断后样式，对齐 helpers 约定）；选中行渲染改用共享 `cursorLine`；移除不可达的 `{[]string{"type"}}` 键位声明
- list.go：`paneBudgets` 收口保证 `left+right+5 <= width`；`Page` 退化语义与 godoc 对齐；`SelectVisible` 空集显式 no-op 并注释
- helpers.go：包级 `max` 改名（如 `maxInt`）消除对 Go 1.21+ 内置的遮蔽；删除 no-op `maxLen`
- config_tab.go：新建表单 `group` 字段补 validate 回调（复用 `enterMetaMode` 校验）
- ai_tab.go：`1039-1040` 重复条件改为区分两个子表达式（对齐原中英文双语匹配意图）
- env_tab.go：删除 `envWithGroup`、`preview :=` 死代码、`_ = i`

无 spec 增量（纯内部缺陷与卫生，无外部行为变化；search 头部截断与窗口化是既有「面板内容截断与详情」需求的实现修正）。

## Impact
- 代码：`internal/tui/{search,list,helpers,config_tab,ai_tab,env_tab}.go` 及对应 `_test.go`
- 审查依据：workspace `docs/reviews/2026-09-12/tui-tab-consistency/review.md`（hygiene 销账清单见 design.md）

## Non-goals
- P1 与耦合 P2 归 `tui-review-fixes-core`（先行实施）
- P3 全部不做；audit_tab 的 P3 打磨项（页步预算、magic number、actRefresh 分组）不在本批

## 验证记录
