## Context

`internal/tui/config_tab.go` 已有列表/编辑（tea.ExecProcess 走 `ConfigEditSession`）；`cmd/interactive_config.go` 为编号菜单。install/uninstall 的核心逻辑在 `internal/config`（Plan*/Execute*），本 change 只做接线与交互。

## Decisions

### D1: TUI 确认用内联视图而非新弹窗组件
沿用 config_tab 现有模式：触发 install/uninstall 后切到 plan 预览态（复用现有列表/详情渲染 + 底部提示 `y` 确认 / `esc` 取消），changed 条目在预览中高亮并对每条再询问。不引入新组件体系。

### D2: 交互菜单增量加项
菜单增加"按组查看 / install / uninstall"项；创建流程追加 group（默认 default）与 description 输入。保持现有编号风格。

### D3: 分组在 TUI 列表的呈现
沿用现有 search/filter 机制：列表项展示 `group/name` 与 description，搜索匹配 group 与 description 字段。是否做分组折叠视图作为 Open Question，首版用平铺 + 前缀。

## Risks / Trade-offs
- [TUI 状态机改动引入回归] → 复用 config_tab 现有测试基建（config_tab_integration_test.go）补覆盖

## Open Questions
- TUI 是否需要分组折叠/树形视图：首版平铺，后续按使用反馈决定。
