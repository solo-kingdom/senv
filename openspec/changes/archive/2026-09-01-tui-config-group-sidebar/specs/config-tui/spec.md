# config-tui Delta

## MODIFIED Requirements

### Requirement: 分组视图与 meta 展示
TUI config tab 与经典交互菜单 SHALL 展示每条配置的分组、描述与保存位置，SHALL 支持按分组浏览或过滤。TUI config tab SHALL 采用双栏布局：左侧为分组选择栏（顶部为 All 伪组，其下为真实分组，各组显示条目计数），右侧为条目列表。默认选中 All，右侧显示全部分组的所有条目（条目仍展示 group 前缀）；选中真实分组时右侧仅显示该分组条目。TUI config tab SHALL 支持在分组栏与条目列表之间切换焦点，焦点切回条目列表时 SHALL 定位到该分组的第一条条目。

#### Scenario: TUI 列表展示分组信息
- **WHEN** 打开 TUI config tab
- **THEN** 每条配置可见 group（All 视图为 `group/name` 前缀）与 description

#### Scenario: 默认全览
- **WHEN** 打开 TUI config tab
- **THEN** 左侧分组栏顶部为 All（默认选中），其下按名称排序列出真实分组及条目计数，右侧显示全部配置的 group/name、description、target path、updated 时间

#### Scenario: 按分组聚焦浏览
- **WHEN** 在左侧分组栏选中某个真实分组
- **THEN** 右侧仅显示该分组的配置条目

#### Scenario: 双栏焦点切换
- **WHEN** 用户按 `←/h` 或 `→/l`
- **THEN** 焦点在分组栏与条目列表之间切换，当前焦点栏有可见的高亮指示

#### Scenario: 经典菜单展示分组信息
- **WHEN** 在经典交互菜单浏览配置
- **THEN** 每条配置展示 group 与 description

### Requirement: 交互式安装与卸载
TUI 与经典菜单 SHALL 提供 install 与 uninstall 入口，作用于选中的单条配置或分组。执行前 SHALL 展示操作计划（动作、目标路径、原因），用户确认后才执行。TUI 中整组作用域 SHALL 锚定左侧分组栏：当焦点在分组栏且选中真实分组时，install/uninstall 作用于该分组；All 伪组不提供整组 install/uninstall 入口。焦点在条目列表时，单条 install/uninstall 作用于光标所在条目，整组 install/uninstall 作用于该条目所属分组。

#### Scenario: TUI 中安装单条配置
- **WHEN** 在 config tab 条目列表对某条配置触发 install
- **THEN** 弹出计划预览，确认后执行并反馈结果

#### Scenario: TUI 中从分组栏安装整组
- **WHEN** 焦点在分组栏且选中真实分组，触发整组 install
- **THEN** 展示该组的 install 计划预览，确认后执行

#### Scenario: All 伪组无整组操作
- **WHEN** 焦点在分组栏且选中 All，触发整组 install/uninstall
- **THEN** 不弹出计划预览，并提示需先选中具体分组

#### Scenario: 经典菜单中按组安装
- **WHEN** 在交互菜单选择按组 install
- **THEN** 展示该组计划，确认后执行

## ADDED Requirements

### Requirement: 搜索跳转定位分组与条目
全局搜索跳转到 config 条目时，config tab SHALL 同时选中该条目所属分组（左栏）与条目本身（右栏）。若该条目属于 default 以外的分组，左栏选中对应真实分组而非 All。

#### Scenario: 跳转定位
- **WHEN** 在全局搜索结果中选择某条 config 条目并确认跳转
- **THEN** overlay 关闭，切换到 config tab，左栏选中该条目所属分组，右栏光标定位到该条目

### Requirement: 双栏下的过滤行为
config tab 的 `/` 过滤 SHALL 作用于条目列表（匹配 name/group/description），左侧分组栏的条目计数 SHALL 随过滤结果更新；不匹配任何条目的分组 SHALL 仍显示但计数为 0。

#### Scenario: 过滤时组计数更新
- **WHEN** 用户输入过滤词且仅部分条目匹配
- **THEN** 右侧仅显示匹配条目，左侧各组计数更新为匹配数量，All 计数为总匹配数
