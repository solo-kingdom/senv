# tui-viewer 增量

## MODIFIED Requirements

### Requirement: Env Tab 浏览与操作

Env Tab SHALL 显示左侧分组列表（含激活标记 `●`）和右侧该分组的环境变量列表。Env Tab MUST 支持完整的环境变量管理操作：浏览、新建、内联编辑、重命名 key、删除、复制、激活/停用分组、新建/重命名/删除分组、解引用视图切换、Tab 内过滤。重命名与删除 SHALL 走存储层的原子操作，MUST NOT 以「新建 + 删除」组合实现。`default` 分组 MUST NOT 被重命名或删除。分组激活/停用统一由 `t` 键切换（重命名键为 `r`，刷新为 `Ctrl+R`，与全局键位语义表一致）。

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
- **WHEN** 用户选中左侧未激活的分组按 `t` 键
- **THEN** 调用 `env.Manager.ActivateGroup`，该分组显示 `●` 标记

#### Scenario: 停用分组
- **WHEN** 用户选中左侧已激活的非默认分组按 `t` 键
- **THEN** 调用 `env.Manager.DeactivateGroup`，移除 `●` 标记（default 分组不可停用）

### Requirement: Text Tab 浏览与操作

Text Tab SHALL 显示左侧分组列表和右侧文本块列表（仅 key/size/更新时间，不显示内容）。Text Tab MUST 支持浏览、新建（vim）、vim 编辑、重命名 key、删除、复制、导出文件、从文件导入、新建/重命名/删除分组、解引用切换、Tab 内过滤。重命名 SHALL 走存储层原子操作。导出文件统一为 `x` 键（与全局导出语义一致）。

#### Scenario: 浏览文本块元信息
- **WHEN** 用户在 Text Tab 选中某分组
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
- **WHEN** 用户在分组栏对某分组触发重命名或删除并确认
- **THEN** 分别调用 `RenameGroup`/`DeleteGroup`，列表刷新，光标落在有效条目上

### Requirement: 错误处理与空状态

TUI SHALL 在操作出错时不崩溃，并通过统一提示条反馈结果：错误优先于警告，警告优先于成功；成功提示 SHALL 超时自动消失，错误与警告 SHALL 由下一次操作或按键清除。所有 Tab 的空列表/空分组 MUST 显示简体中文空状态提示。面向用户的界面文案 SHALL 统一为简体中文，键位名、命令名与技术标识保留原文。

#### Scenario: 操作出错不崩溃
- **WHEN** 某 Manager 方法返回错误（如解密失败、key 不存在）
- **THEN** 提示条显示错误信息，当前视图保留，不退出 TUI

#### Scenario: 成功提示自动消失
- **WHEN** 用户成功执行一次写操作
- **THEN** 提示条显示成功信息，超时后自动消失，无需按键

#### Scenario: 空分组空状态
- **WHEN** 用户选中一个没有任何条目的分组
- **THEN** 列表区域显示简体中文空状态提示（如"该分组暂无环境变量"），不显示空白

#### Scenario: 空状态覆盖全部 Tab
- **WHEN** 用户切换到 SSH Tab 且没有任何 host 或 keypair
- **THEN** 显示空状态提示说明如何创建（如"执行 senv host add 添加后按 Ctrl+R 刷新"），不显示空白面板

#### Scenario: 错误条清除
- **WHEN** 错误条显示后用户执行下一次操作
- **THEN** 错误条被清除或在新操作成功后消失

### Requirement: 键位总览

TUI SHALL 提供键位总览 overlay（触发键 `?`），列出全局键位与当前 Tab 的键位，`?` 或 `esc` SHALL 关闭它。文本输入模式（`InputMode`）下 `?` MUST 作为普通字符输入而不触发 overlay。键位总览与状态栏提示 SHALL 由中央 keymap 注册表生成，列出的键位与说明 MUST 和实际行为一致。同一动作在不同 Tab MUST 使用相同按键：重命名统一为 `r`，刷新统一为 `Ctrl+R`，确认统一为 `y`/`enter`、取消统一为 `esc`/`n`（计划确认页中除 `esc`/`n`/`y`/`enter`/`F` 外的按键 MUST 被忽略，MUST NOT 被解释为取消或放行）。

#### Scenario: 打开键位总览
- **WHEN** 用户按 `?`
- **THEN** 弹出 overlay，分「全局」与当前 Tab 两组列出键位与说明

#### Scenario: 输入模式不劫持
- **WHEN** 用户正在表单/过滤输入框中输入 `?`
- **THEN** `?` 作为字符进入输入框，不打开 overlay

#### Scenario: 键位说明与实际行为一致
- **WHEN** 任一 Tab 的键位总览列出某按键与动作
- **THEN** 在该 Tab 按下该键执行所述动作；同一动作（如重命名、刷新）在所有列表 Tab 使用同一按键
