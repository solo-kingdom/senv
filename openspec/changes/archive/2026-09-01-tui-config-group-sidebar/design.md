# Design: tui-config-group-sidebar

## Context

config tab（`internal/tui/config_tab.go`）目前是单列扁平列表：`items []configRow` 按组排序后渲染，`group/name` 作为行前缀。env/text tab 已实现双栏模式（左栏组列表 + 右栏条目，`focusLeft` 切换，`leftW = width/4` clamp 到 16~26）。config 数据层分组能力已就绪（`Manager.Groups()`、`List(groupFilter)`），本变更纯为 TUI 呈现层重构。

## Goals / Non-Goals

**Goals:**
- config tab 双栏化，形态与 env/text 对齐，默认 All 全览
- 整组 install/uninstall 锚定左栏选中组
- 搜索跳转携带 group 定位

**Non-Goals:**
- 不提取三 tab 公共 sidebar 组件（见 Decisions）
- 不动经典交互菜单（`cmd/interactive_config.go` 已满足分组展示要求）

## Decisions

### D1: 数据模型改为 groups + itemsByGroup
对齐 env/text tab：`groups []configGroupRow{name, count}`（索引 0 固定为 All 伪组），`itemsByGroup map[string][]configRow` 缓存每组条目，All 视图的条目列表由全组拼接（保留排序：按组再按名）。
替代方案：保留扁平 `items` + 当前组过滤字段。否决理由：与 env/text 形态不一致会加大后续维护成本，且组计数需要按组聚合，本质是同一份索引。

### D2: All 伪组作为索引 0 的真实行，而非特例分支
`groups[0]` 恒为 All（name 用常量 `allGroup = "All"`，与真实组名不冲突——存储层组名默认 `default`，且 CLI 侧无 "All" 保留字冲突的组命名约束问题，因伪组不进数据层）。计数为总条目数。选中 All 时 `I/U` 提示"需先选中具体分组"。
替代方案：All 作为 `groupIndex == -1` 特例。否决理由：渲染与导航会到处分叉，伪组实体化后导航逻辑零特例。

### D3: 焦点与导航复用 env/text 模式
`focusLeft bool`，`←→/hl` 切栏，左栏 `↑↓/jk` 移动组光标，右栏移动条目光标；切组时 `itemIndex` 归零并定位该组首条。`g/G` 在右栏跳首/尾，左栏跳首/尾组（含 All）。
理由：用户肌肉记忆跨 tab 一致，实现可直接参照 text_tab。

### D4: 整组操作锚定左栏，右栏保持现有键位
左栏选中真实组时 `I/U` → `enterPlan(kind, groupScope)`，scope 取左栏组；右栏 `i/u`（单条）、`I/U`（条目所属组）语义不变。All 上 `I/U` 仅 flash 提示。
理由：左栏即组作用域的可见表达，比"光标条目的组"更直观；同时不破坏右栏既有操作。

### D5: 搜索跳升级为 focusJump(group, name)
`searchJumpMsg` 已携带 group；`model.go` 的 `applyJump` config 分支改为 `ct.focusJump(j.group, j.name)`。`focusJump` 内：若 group 为空或找不到组，落回 All 再按 name 定位（防御性，保证旧消息/异常数据不崩溃）。
理由：spec 要求跳转同时选中分组；空组回落保证健壮。

### D6: 过滤仅作用于条目，组计数实时聚合
`filteredItems()` 逻辑保留但按当前选中组切片；左栏计数在过滤态下显示各组匹配数（All 显示总匹配数）。不匹配组保留显示（计数 0），避免过滤时左栏抖动。
替代方案：过滤时隐藏空组。否决理由：组列表随键入跳动会干扰定位，计数为 0 已足够表达。

### D7: 不提取公共 sidebar 组件
本变更将产生第三处"左栏组列表"实现（宽度 clamp、组行渲染、focusLeft 切换）。提取公共组件需同时改动 env/text tab 的结构体与测试，扩散面大、回归风险高，违背项目"保持简洁、按 module 分步实现"的约束。三处稳定后再提取（届时接口已被三次验证）。
Trade-off：接受短期重复，换取变更隔离。

## Risks / Trade-offs

- [create 流程后光标定位]：新建配置落在某组，双栏下需决定跳到哪 → 沿用现有逻辑重载后按 name 定位，落在该条目所属组（而非 All），与搜索跳转行为一致
- [左栏宽度挤压右栏 target path 显示]：窄终端下右栏变窄 → 沿用 text_tab 的 clamp（16~26），右栏列宽按 `truncRunes`/`truncPath` 自适应收缩，已有先例
- [组数极多时左栏滚动]：组列表无 viewport 滚动 → 与 env/text 现状一致（同期限制），超出高度截断，留待后续统一处理

## Migration Plan

纯 UI 层变更，无数据/存储/CLI 迁移。发布即生效，回滚 = revert commit。
