# config-tui Specification

## Purpose
在 TUI 与经典交互菜单中提供 config 的分组视图、meta 展示，以及带计划预览与确认的 install / uninstall 操作入口。
## Requirements
### Requirement: 分组视图与 meta 展示
TUI config tab 与经典交互菜单 SHALL 展示每条配置的分组、描述与保存位置，SHALL 支持按分组浏览或过滤。

#### Scenario: TUI 列表展示分组信息
- **WHEN** 打开 TUI config tab
- **THEN** 列表中可见每条配置的 group 与 description

### Requirement: 交互式安装与卸载
TUI 与经典菜单 SHALL 提供 install 与 uninstall 入口，作用于选中的单条配置或分组。执行前 SHALL 展示操作计划（动作、目标路径、原因），用户确认后才执行。

#### Scenario: TUI 中安装单条配置
- **WHEN** 在 config tab 对某条配置触发 install
- **THEN** 弹出计划预览，确认后执行并反馈结果

#### Scenario: 经典菜单中按组安装
- **WHEN** 在交互菜单选择按组 install
- **THEN** 展示该组计划，确认后执行

### Requirement: 改动条目的确认
uninstall 计划中标记为 changed（目标文件被本地改动）的条目 SHALL 需要显式确认后才删除。

#### Scenario: 改动文件需确认
- **WHEN** uninstall 计划含 changed 条目且用户确认计划
- **THEN** 对 changed 条目再次单独确认，拒绝则保留文件

