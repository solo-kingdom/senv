# tui-viewer Delta

## MODIFIED Requirements

### Requirement: Env Tab 浏览与操作

Env Tab SHALL 显示左侧分组列表（含激活标记 `●`）和右侧该分组的环境变量列表。Env Tab MUST 支持完整的环境变量管理操作：浏览、新建、内联编辑、重命名 key、删除、复制、激活/停用分组、新建/重命名/删除分组、解引用视图切换、Tab 内过滤。重命名与删除 SHALL 走存储层的原子操作，MUST NOT 以「新建 + 删除」组合实现。`default` 分组 MUST NOT 被重命名或删除。

#### Scenario: 浏览分组的环境变量
- **WHEN** 用户在 Env Tab 选中左侧某分组
- **THEN** 右侧显示该分组所有环境变量的 key=value（值默认遮蔽），激活的分组在左侧显示 `●` 标记

#### Scenario: 内联编辑环境变量
- **WHEN** 用户选中某环境变量按 `e` 键
- **THEN** 弹出内联输入框（预填当前值），用户修改并确认后调用 `env.Manager.Set` 保存，列表刷新

#### Scenario: 新建环境变量
- **WHEN** 用户按 `n` 键
- **THEN** 弹出输入框依次收集 key 和 value，调用 `env.Manager.Set` 保存到当前分组

#### Scenario: 删除环境变量
- **WHEN** 用户选中某变量按 `d` 键
- **THEN** 弹出确认提示，确认后调用 `env.Manager.Delete` 删除，列表刷新

#### Scenario: 重命名环境变量
- **WHEN** 用户选中某变量按重命名键并输入新 key（不与组内既有 key 冲突）
- **THEN** 调用存储层 rename 原子改名，值不变，列表刷新且光标停在新 key 上

#### Scenario: 重命名冲突
- **WHEN** 用户输入的新 key 已存在于当前分组
- **THEN** 表单内联提示冲突，保持打开且不写入

#### Scenario: 重命名分组
- **WHEN** 用户在分组栏对非 default 分组触发重命名并输入新名称
- **THEN** 分组改名且其条目与激活态保持不变

#### Scenario: 删除分组
- **WHEN** 用户在分组栏对非 default 分组触发删除并确认
- **THEN** 该分组及其条目被删除，列表刷新；删除激活分组时确认提示 SHALL 额外说明其激活态将被移除

#### Scenario: 激活分组
- **WHEN** 用户选中左侧未激活的分组按 `a` 键
- **THEN** 调用 `env.Manager.ActivateGroup`，该分组显示 `●` 标记

#### Scenario: 停用分组
- **WHEN** 用户选中左侧已激活的非默认分组按 `x` 键
- **THEN** 调用 `env.Manager.DeactivateGroup`，移除 `●` 标记（default 分组不可停用）

### Requirement: Text Tab 浏览与操作

Text Tab SHALL 显示左侧分组列表和右侧文本块列表（仅 key/size/更新时间，不显示内容）。Text Tab MUST 支持浏览、新建（vim）、vim 编辑、重命名 key、删除、复制、导出文件、从文件导入、新建/重命名/删除分组、解引用切换、Tab 内过滤。重命名 SHALL 走存储层原子操作。

#### Scenario: 浏览文本块元信息
- **WHEN** 用户在 Text Tab 选中某分组
- **THEN** 右侧显示该分组所有文本块的 key、大小（字节）、更新时间，不显示内容

#### Scenario: 用 vim 编辑文本块
- **WHEN** 用户选中某文本块按 `e` 键
- **THEN** TUI 通过 `tea.ExecProcess` 挂起，调用 `text.Manager.SetViaEditor` 打开 vim（预填现有内容），vim 退出后恢复 TUI，若有改动则重新加密保存并刷新列表

#### Scenario: 导出文本块到文件
- **WHEN** 用户选中某文本块按 `o` 键并指定路径
- **THEN** 调用 `text.Manager.GetToFile` 写入指定路径

#### Scenario: 从文件导入文本块
- **WHEN** 用户触发导入并给出源文件路径与目标 key
- **THEN** 调用 `text.Manager.SetFromFile` 加密存储，列表刷新

#### Scenario: 重命名文本块
- **WHEN** 用户对选中文本块触发重命名并输入不冲突的新 key
- **THEN** 调用存储层 rename 原子改名，内容不变，列表刷新

#### Scenario: 分组重命名与删除
- **WHEN** 用户在分组栏对某分组触发重命名或删除并确认
- **THEN** 分别调用 `RenameGroup`/`DeleteGroup`，列表刷新，光标落在有效条目上

### Requirement: Config Tab 浏览与操作

Config Tab SHALL 采用单栏列表布局（无左侧分组栏），显示所有配置文件的 name、target 路径、更新时间。Config Tab MUST 支持浏览、创建（从文件导入）、vim 编辑、重命名条目、编辑元信息（分组、描述）、导出到 target、删除、查看详情、Tab 内过滤。重命名 SHALL 走存储层原子操作，MUST NOT 改变条目的 target 路径与内容。

#### Scenario: 浏览配置文件列表
- **WHEN** 用户切换到 Config Tab
- **THEN** 单栏显示所有配置文件的 name、target 路径、更新时间

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
