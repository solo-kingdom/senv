# Tasks: tui-config-group-sidebar

## 1. 数据模型重构

- [x] 1.1 configTab 引入 `groups []configGroupRow`（索引 0 为 All 伪组，含计数）与 `itemsByGroup map[string][]configRow`，替换扁平 `items`
- [x] 1.2 load 流程调用 `Manager.Groups()` + `List("")` 聚合分组与条目；保留 loaded 懒加载语义
- [x] 1.3 `filteredItems()` 按当前选中组切片（All = 全组拼接，按组+名排序），过滤匹配逻辑不变

## 2. 双栏渲染与导航

- [x] 2.1 `renderList` 拆分为左栏 `renderGroups` + 右栏 `renderItems`，宽度计算沿用 text_tab clamp（16~26）
- [x] 2.2 新增 `focusLeft`，`←→/hl` 切栏，左栏 j/k 移组、右栏 j/k 移条目，切组时 itemIndex 归零
- [x] 2.3 `g/G` 按当前焦点栏跳首/尾；左栏高亮与右栏选中样式对齐 env/text tab

## 3. 组作用域操作

- [x] 3.1 左栏选中真实组时 `I/U` 调 `enterPlan(kind, true)`，scope 取左栏组；All 上 `I/U` flash 提示"需先选中具体分组"
- [x] 3.2 右栏 `i/u/I/U` 语义保持不变（I/U 作用光标条目所属组）
- [x] 3.3 create 成功后重载并定位到新条目所属组与该条目

## 4. 搜索跳转接线

- [x] 4.1 `focusJump(name)` 改为 `focusJump(group, name)`：定位左栏组 + 右栏条目；组为空/不存在时回落 All 再按 name 定位
- [x] 4.2 `model.go` applyJump config 分支传入 `j.group`

## 5. 过滤适配

- [x] 5.1 过滤态下左栏组计数显示各组匹配数（All 为总匹配数），空匹配组保留显示计数 0
- [x] 5.2 过滤词清空后恢复真实计数

## 6. 测试

- [x] 6.1 更新 `config_tab_test.go` / `config_tab_integration_test.go`：双栏渲染、焦点切换、组选择、All 默认选中
- [x] 6.2 新增：左栏 I/U 组作用域 plan、All 上 I/U 提示、搜索跳转 focusJump(group, name)、过滤下组计数
- [x] 6.3 `make check` 全绿（fmt + vet + lint + test -race）
