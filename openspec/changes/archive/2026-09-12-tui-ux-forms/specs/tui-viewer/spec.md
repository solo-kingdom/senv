# tui-viewer 增量

## MODIFIED Requirements

### Requirement: Env Tab 浏览与操作

Env Tab SHALL 采用双栏分组侧栏布局：左侧为分组侧栏（顶部为 All 伪组且默认选中，其下为分组列表——default 分组置顶并带 `(default)` 标识，激活分组显示 `●`），各组显示条目计数且计数随 `/` 过滤结果更新；右侧为环境变量列表。All 视图显示全部分组的环境变量（行前缀 `group/key`）；选中真实分组时右侧仅显示该分组。`←→/hl` SHALL 在侧栏与列表间切换焦点，切到列表时定位到该分组第一条。Env Tab MUST 支持完整的环境变量管理操作：浏览、新建、内联编辑、重命名 key、删除、复制、激活/停用分组、新建/重命名/删除分组、解引用视图切换、Tab 内过滤。重命名与删除 SHALL 走存储层的原子操作，MUST NOT 以「新建 + 删除」组合实现。`default` 分组 MUST NOT 被重命名或删除。分组激活/停用统一由 `t` 键切换。新建 SHALL 走结构化表单，value 以遮蔽输入收集，明文 MUST NOT 进入任何渲染文本。

#### Scenario: 默认全览
- **WHEN** 打开 Env Tab
- **THEN** 左侧侧栏顶部为 All（默认选中），其下按 default 置顶、名称排序列出分组及条目计数，右侧显示全部环境变量并带 `group/key` 前缀

#### Scenario: 侧栏计数随过滤更新
- **WHEN** 用户输入过滤词且仅部分变量匹配
- **THEN** 右侧仅显示匹配条目，侧栏各组计数更新为匹配数量，All 计数为总匹配数

#### Scenario: 浏览分组的环境变量
- **WHEN** 用户在左侧侧栏选中某分组
- **THEN** 右侧显示该分组所有环境变量的 key=value（值默认遮蔽），激活的分组在侧栏显示 `●` 标记

#### Scenario: 内联编辑环境变量
- **WHEN** 用户选中某环境变量按 `e` 键
- **THEN** 弹出内联输入框（预填当前值），用户修改并确认后调用 `env.Manager.Set` 保存，列表刷新

#### Scenario: 新建环境变量
- **WHEN** 用户按 `n` 键
- **THEN** 弹出结构化表单收集 key 与 value（value 为遮蔽输入），key 组内冲突或为空时内联报错、表单保持打开；校验通过后调用 `env.Manager.Set` 保存到当前分组，列表刷新

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
- **WHEN** 用户在侧栏对非 default 分组触发重命名并输入新名称
- **THEN** 分组改名且其条目与激活态保持不变

#### Scenario: 删除分组
- **WHEN** 用户在侧栏对非 default 分组触发删除并确认
- **THEN** 该分组及其条目被删除，列表刷新；删除激活分组时确认提示 SHALL 额外说明其激活态将被移除

#### Scenario: 激活分组
- **WHEN** 用户选中侧栏未激活的分组按 `t` 键
- **THEN** 调用 `env.Manager.ActivateGroup`，该分组显示 `●` 标记

#### Scenario: 停用分组
- **WHEN** 用户选中侧栏已激活的非默认分组按 `t` 键
- **THEN** 调用 `env.Manager.DeactivateGroup`，移除 `●` 标记（default 分组不可停用）

### Requirement: Config Tab 浏览与操作

Config Tab SHALL 采用双栏分组浏览布局：左侧为分组侧栏（顶部 All 伪组，其下真实分组与条目计数），右侧为条目列表，详细行为见 config-tui 能力规约。Config Tab MUST 支持浏览、创建（从文件导入，走结构化表单）、vim 编辑、重命名条目、编辑元信息（分组、描述）、导出到 target、删除、查看详情、Tab 内过滤。重命名 SHALL 走存储层原子操作，MUST NOT 改变条目的 target 路径与内容。

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
- **WHEN** 用户按 `n` 键并在表单中填写 name、源文件路径、target 路径、分组与描述后提交
- **THEN** 必填缺失或 name 冲突时内联报错且表单保持打开；校验通过后调用 `config.Manager.Create` 加密导入，列表刷新

#### Scenario: 重命名配置条目
- **WHEN** 用户对选中配置触发重命名并输入不冲突的新 name
- **THEN** 调用存储层 rename，target 路径与内容不变，列表刷新

#### Scenario: 编辑配置元信息
- **WHEN** 用户触发元信息编辑并修改分组或描述
- **THEN** 调用 `config.Manager.SetMeta` 保存，详情与列表展示更新后的分组与描述
