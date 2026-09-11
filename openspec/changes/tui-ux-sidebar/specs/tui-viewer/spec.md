# tui-viewer 增量

## MODIFIED Requirements

### Requirement: Env Tab 浏览与操作

Env Tab SHALL 采用双栏分组侧栏布局：左侧为分组侧栏（顶部为 All 伪组且默认选中，其下为分组列表——default 分组置顶并带 `(default)` 标识，激活分组显示 `●`），各组显示条目计数且计数随 `/` 过滤结果更新；右侧为环境变量列表。All 视图显示全部分组的环境变量（行前缀 `group/key`）；选中真实分组时右侧仅显示该分组。`←→/hl` SHALL 在侧栏与列表间切换焦点，切到列表时定位到该分组第一条。Env Tab MUST 支持完整的环境变量管理操作：浏览、新建、内联编辑、重命名 key、删除、复制、激活/停用分组、新建/重命名/删除分组、解引用视图切换、Tab 内过滤。重命名与删除 SHALL 走存储层的原子操作，MUST NOT 以「新建 + 删除」组合实现。`default` 分组 MUST NOT 被重命名或删除。分组激活/停用统一由 `t` 键切换。

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

### Requirement: Text Tab 浏览与操作

Text Tab SHALL 采用双栏分组侧栏布局：左侧为分组侧栏（顶部为 All 伪组且默认选中，其下为分组列表——default 分组置顶），各组显示文本块计数且计数随 `/` 过滤结果更新，空分组 SHALL 显示且计数为 0；右侧为文本块列表（仅 key/size/更新时间，不显示内容）。All 视图显示全部分组的文本块（行前缀 `group/key`）；选中真实分组时右侧仅显示该分组。`←→/hl` SHALL 在侧栏与列表间切换焦点。Text Tab MUST 支持浏览、新建（vim）、vim 编辑、重命名 key、删除、复制、导出文件、从文件导入、新建/重命名/删除分组、解引用切换、Tab 内过滤。重命名 SHALL 走存储层原子操作。导出文件统一为 `x` 键（与全局导出语义一致）。

#### Scenario: 默认全览
- **WHEN** 打开 Text Tab
- **THEN** 左侧侧栏顶部为 All（默认选中），其下列出分组及文本块计数（空分组计数 0），右侧显示全部文本块并带 `group/key` 前缀

#### Scenario: 浏览文本块元信息
- **WHEN** 用户在侧栏选中某分组
- **THEN** 右侧显示该分组所有文本块的 key、大小（字节）、更新时间，不显示内容

#### Scenario: 用 vim 编辑文本块
- **WHEN** 用户选中某文本块按 `e` 键
- **THEN** TUI 通过 `tea.ExecProcess` 挂起，调用 `text.Manager.SetViaEditor` 打开 vim（预填现有内容），vim 退出后恢复 TUI，若有改动则重新加密保存并刷新列表

#### Scenario: 导出文本块到文件
- **WHEN** 用户选中某文本块按 `x` 键并指定路径
- **THEN** 调用 `text.Manager.GetToFile` 写入指定路径

#### Scenario: 从文件导入文本块
- **WHEN** 用户触发导入并给出源文件路径与目标 key
- **THEN** 调用 `text.Manager.SetFromFile` 加密存储，列表刷新

#### Scenario: 重命名文本块
- **WHEN** 用户对选中文本块触发重命名并输入不冲突的新 key
- **THEN** 调用存储层 rename 原子改名，内容不变，列表刷新

#### Scenario: 分组重命名与删除
- **WHEN** 用户在侧栏对某分组触发重命名或删除并确认
- **THEN** 分别调用 `RenameGroup`/`DeleteGroup`，列表刷新，光标落在有效条目上
