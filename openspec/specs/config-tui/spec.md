# config-tui Specification

## Purpose
在 TUI 与经典交互菜单中提供 config 的分组视图、meta 展示，以及带计划预览与确认的 install / uninstall 操作入口。
## Requirements
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

### Requirement: 改动条目的确认
uninstall 计划中标记为 changed（目标文件被本地改动）的条目 SHALL 需要显式确认后才删除。

#### Scenario: 改动文件需确认
- **WHEN** uninstall 计划含 changed 条目且用户确认计划
- **THEN** 对 changed 条目再次单独确认，拒绝则保留文件

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

### Requirement: 安装与卸载入口

TUI 与经典菜单 SHALL 提供 install 与 uninstall 入口，作用于选中的单条配置或分组。执行前 SHALL 展示操作计划（动作、目标路径、原因），用户确认后才执行。TUI 中整组作用域 SHALL 锚定左侧分组栏：当焦点在分组栏时，install/uninstall 作用于选中分组——选中真实分组时范围为该分组条目，选中 All 伪组时范围为全部条目（计划逐条列出，语义与真实分组一致）。焦点在条目列表时，单条 install/uninstall 作用于光标所在条目，整组 install/uninstall 作用于该条目所属分组。

#### Scenario: TUI 中安装单条配置
- **WHEN** 在 config tab 条目列表对某条配置触发 install
- **THEN** 弹出计划预览，确认后执行并反馈结果

#### Scenario: TUI 中从分组栏安装整组
- **WHEN** 焦点在分组栏且选中真实分组，触发整组 install
- **THEN** 展示该组的 install 计划预览，确认后执行

#### Scenario: All 伪组整组操作
- **WHEN** 焦点在分组栏且选中 All，触发整组 install/uninstall
- **THEN** 弹出以全部条目为范围的计划预览，确认后执行

#### Scenario: 经典菜单中按组安装
- **WHEN** 在交互菜单选择按组 install
- **THEN** 展示该组计划，确认后执行

### Requirement: 多选批量安装与卸载

config tab 条目列表 SHALL 支持多选集（`space` 勾选、`a` 全选可见集，语义见 tui-viewer「多选集与批量操作」）。`i`/`u` 在多选集非空时 SHALL 以选择集为范围生成一份合并计划预览（可跨分组，逐条列出动作、目标路径与原因），确认后执行；changed 条目的逐条确认语义与既有要求一致。多选集为空时保持既有单条/整组语义。scope 快捷键 `I`/`U`（整组/全部）SHALL 保留，与多选集并存互不替代。

#### Scenario: All 视图跨组勾选批量安装
- **WHEN** 用户在 All 视图勾选分属 3 个分组的 3 条配置按 `i`
- **THEN** 弹出一份合并计划逐条列出 3 条目标，确认后全部执行

#### Scenario: 空集回落单条
- **WHEN** 用户未勾选任何条目按 `u`
- **THEN** 仅对光标所在条目弹出 uninstall 计划，行为与既有要求一致

#### Scenario: scope 键不受影响
- **WHEN** 焦点在侧栏真实分组按 `I` 触发整组 install
- **THEN** 以该分组为范围弹计划（即使条目列表存在勾选集），两者语义独立

### Requirement: 创建配置走结构化表单

config tab 的 `n` 创建 SHALL 使用结构化表单一次收集：name（必填，重名冲突内联报错）、源文件路径（必填，存在性校验）、target 路径（必填）、分组（从既有分组选择，可空 = default）、描述（可选）。表单契约（`tab`/`shift+tab` 导航、内联校验不丢输入、`esc` 取消零副作用、输入模式隔离全局键）遵循 tui-forms 能力规约；提交失败 SHALL 经 reopen 模式回填表单修正，MUST NOT 落盘部分状态。

#### Scenario: 表单创建成功
- **WHEN** 用户按 `n` 填写 name、源文件路径、target 路径、分组与描述后提交
- **THEN** 调用 `config.Manager.Create` 加密导入，列表刷新并出现新条目

#### Scenario: 必填缺失内联报错
- **WHEN** 用户未填源文件路径直接提交
- **THEN** 该字段旁内联报错，表单保持打开且已填内容不丢失，不发生写入

#### Scenario: 取消零副作用
- **WHEN** 用户在创建表单按 `esc`
- **THEN** 表单关闭，不创建条目、不读源文件，列表不变

