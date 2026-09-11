# tui-viewer 增量

## MODIFIED Requirements

### Requirement: Config Tab 浏览与操作

Config Tab SHALL 采用双栏分组浏览布局：左侧为分组侧栏（顶部 All 伪组，其下真实分组与条目计数），右侧为条目列表，详细行为见 config-tui 能力规约。Config Tab MUST 支持浏览、创建（从文件导入）、vim 编辑、重命名条目、编辑元信息（分组、描述）、导出到 target、删除、查看详情、Tab 内过滤。重命名 SHALL 走存储层原子操作，MUST NOT 改变条目的 target 路径与内容。

#### Scenario: 浏览配置文件列表
- **WHEN** 用户切换到 Config Tab
- **THEN** 左侧分组侧栏默认选中 All，右侧显示全部配置条目的 group/name、target 路径、更新时间

#### Scenario: 用 vim 编辑配置文件
- **WHEN** 用户选中某配置按 `e` 键
- **THEN** TUI 通过 `tea.ExecProcess` 挂起，调用 `config.Manager.Edit` 打开 vim，vim 退出后恢复 TUI，若有改动则重新加密保存

#### Scenario: 导出配置到 target 路径
- **WHEN** 用户选中某配置按 `x` 键
- **THEN** 调用 `config.Manager.Export` 解密写回该配置的 target 路径

#### Scenario: 创建配置（从文件导入）
- **WHEN** 用户按 `n` 键并依次输入 name、源文件路径、target 路径
- **THEN** 调用 `config.Manager.Create` 加密导入

#### Scenario: 重命名配置条目
- **WHEN** 用户对选中配置触发重命名并输入不冲突的新 name
- **THEN** 调用存储层 rename，target 路径与内容不变，列表刷新

#### Scenario: 编辑配置元信息
- **WHEN** 用户触发元信息编辑并修改分组或描述
- **THEN** 调用 `config.Manager.SetMeta` 保存，详情与列表展示更新后的分组与描述
