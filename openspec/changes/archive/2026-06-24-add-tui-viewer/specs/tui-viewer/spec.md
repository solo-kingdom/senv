## ADDED Requirements

### Requirement: TUI 启动命令

系统 SHALL 提供 `senv tui` 命令，启动全屏 TUI 界面浏览 env/text/config 数据。启动时 MUST 复用现有密码校验与 session 缓存机制。

#### Scenario: 项目已初始化且密码正确
- **WHEN** 用户运行 `senv tui` 且项目已初始化，输入正确密码（或 session 缓存有效）
- **THEN** 系统进入全屏 TUI，默认显示 Env Tab 的 default 分组内容

#### Scenario: 项目未初始化
- **WHEN** 用户运行 `senv tui` 但项目未初始化
- **THEN** 系统提示"项目未初始化，请先运行 senv init"并退出，不进入 TUI

#### Scenario: 密码错误
- **WHEN** 用户运行 `senv tui` 输入错误密码
- **THEN** 系统提示"密码错误"并退出，不进入 TUI

### Requirement: Tab 切换

TUI SHALL 提供三个标签页：Env、Text、Config。用户 MUST 能通过 `Tab` 键或数字键 `1`/`2`/`3` 在标签间切换。每个 Tab MUST 有专属于该数据类型的布局和动作栏。

#### Scenario: 切换标签
- **WHEN** 用户在 Env Tab 按下 `Tab` 键或 `2` 键
- **THEN** 界面切换到 Text Tab，显示 text 分组与文本块列表

#### Scenario: 保留导航状态
- **WHEN** 用户从 Env Tab 切换到 Text Tab 再切回 Env Tab
- **THEN** Env Tab 恢复之前选中的分组和条目（导航状态不丢失）

### Requirement: Env Tab 浏览与操作

Env Tab SHALL 显示左侧分组列表（含激活标记 `●`）和右侧该分组的环境变量列表。Env Tab MUST 支持完整的环境变量管理操作：浏览、新建、内联编辑、删除、复制、激活/停用分组、新建分组、解引用视图切换、Tab 内过滤。

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

#### Scenario: 激活分组
- **WHEN** 用户选中左侧未激活的分组按 `a` 键
- **THEN** 调用 `env.Manager.ActivateGroup`，该分组显示 `●` 标记

#### Scenario: 停用分组
- **WHEN** 用户选中左侧已激活的非默认分组按 `x` 键
- **THEN** 调用 `env.Manager.DeactivateGroup`，移除 `●` 标记（default 分组不可停用）

### Requirement: Text Tab 浏览与操作

Text Tab SHALL 显示左侧分组列表和右侧文本块列表（仅 key/size/更新时间，不显示内容）。Text Tab MUST 支持浏览、新建（vim）、vim 编辑、删除、复制、导出文件、新建分组、解引用切换、Tab 内过滤。

#### Scenario: 浏览文本块元信息
- **WHEN** 用户在 Text Tab 选中某分组
- **THEN** 右侧显示该分组所有文本块的 key、大小（字节）、更新时间，不显示内容

#### Scenario: 用 vim 编辑文本块
- **WHEN** 用户选中某文本块按 `e` 键
- **THEN** TUI 通过 `tea.ExecProcess` 挂起，调用 `text.Manager.SetViaEditor` 打开 vim（预填现有内容），vim 退出后恢复 TUI，若有改动则重新加密保存并刷新列表

#### Scenario: 导出文本块到文件
- **WHEN** 用户选中某文本块按 `o` 键并指定路径
- **THEN** 调用 `text.Manager.GetToFile` 写入指定路径

### Requirement: Config Tab 浏览与操作

Config Tab SHALL 采用单栏列表布局（无左侧分组栏），显示所有配置文件的 name、target 路径、更新时间。Config Tab MUST 支持浏览、创建（从文件导入）、vim 编辑、导出到 target、删除、查看详情、Tab 内过滤。

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

### Requirement: 敏感值遮蔽

TUI SHALL 对敏感值默认遮蔽，防止肩窥。遮蔽策略 MUST 按类型区分：env 值在列表中永远遮蔽（显示为 `***`），text/config 列表只显示元信息不显示内容。用户 MUST 能通过 `v` 键单条切换当前选中 env 值的明文/遮蔽状态。

#### Scenario: env 值默认遮蔽
- **WHEN** 用户浏览 Env Tab 的变量列表
- **THEN** 所有 env 值显示为 `***`（或长值截断为 `prefix***`），不显示明文

#### Scenario: 单条切换 env 明文
- **WHEN** 用户选中某 env 变量按 `v` 键
- **THEN** 仅该条变量的值显示明文，其他变量仍遮蔽

#### Scenario: 移开光标自动重新遮蔽
- **WHEN** 用户已用 `v` 解开某条明文，然后将光标移到另一条
- **THEN** 之前解开的变量自动恢复遮蔽状态

#### Scenario: text/config 内容不进列表
- **WHEN** 用户浏览 Text Tab 或 Config Tab
- **THEN** 列表只显示 key/size/time 或 name/target，不显示任何内容片段（内容仅在 vim 编辑或详情面板中可见）

### Requirement: 解引用视图切换

Env Tab 和 Text Tab SHALL 默认显示原始存储值（含 `{{env:...}}`/`{{text:...}}` 引用语法）。用户 MUST 能通过 `D` 键切换到解引用后的视图。

#### Scenario: 默认显示原始值
- **WHEN** 用户浏览含交叉引用的 env/text 条目
- **THEN** 值显示原始存储形式（如 `postgres://user:{{text:secrets:DB_PASS}}@host`）

#### Scenario: 切换到解引用视图
- **WHEN** 用户按 `D` 键
- **THEN** 所有显示的值经过解引用处理（引用被替换为实际值），再次按 `D` 切回原始视图

#### Scenario: 解引用失败
- **WHEN** 用户切换解引用视图但某引用指向不存在的条目
- **THEN** 解析失败的条目显示错误标记或保留原始引用语法，错误条提示具体未解析的引用

### Requirement: Tab 内过滤

每个 Tab SHALL 支持按 ` / ` 键触发当前 Tab 内的过滤，仅匹配 key/name（不匹配值）。

#### Scenario: 过滤当前列表
- **WHEN** 用户在 Env Tab 按 `/` 键并输入 `DATABASE`
- **THEN** 右侧列表仅显示 key 含 `DATABASE` 的环境变量（忽略大小写）

#### Scenario: 清除过滤
- **WHEN** 用户清空过滤输入框或按 `esc`
- **THEN** 列表恢复显示全部分组内的条目

### Requirement: 全局跨类型搜索

TUI SHALL 提供全局搜索 overlay（触发键 `S`），跨 Env/Text/Config 三类数据搜索。搜索 MUST 只匹配 key/name，绝不匹配值。搜索结果 MUST 标识条目类型，并支持跳转定位。

#### Scenario: 触发全局搜索
- **WHEN** 用户按 `S` 键
- **THEN** 弹出全局搜索 overlay，含输入框和跨类型结果列表

#### Scenario: 搜索结果按类型展示
- **WHEN** 用户输入 `database` 且数据中存在匹配的 env key、text key、config name
- **THEN** 结果列表显示所有匹配项，每项标注类型（Env/Text/Cfg）、分组（若适用）、key/name，值部分遮蔽（env 显示 `***`，text 显示 size，config 显示 target）

#### Scenario: 搜索不匹配值
- **WHEN** 用户输入某个仅出现在值中而不在任何 key/name 中的字符串
- **THEN** 结果列表为空（显示"无匹配"），不返回任何值匹配

#### Scenario: 跳转定位
- **WHEN** 用户在搜索结果选中某条按 `enter`
- **THEN** overlay 关闭，切换到该条目所属的 Tab，选中对应分组（若有）和条目

#### Scenario: 关闭搜索
- **WHEN** 用户按 `esc`
- **THEN** overlay 关闭，返回之前的 Tab 视图

### Requirement: vim 编辑复用现有加密闭环

text/config 的 vim 编辑 MUST 复用现有 `text.Manager.SetViaEditor` 和 `config.Manager.Edit` 的解密→临时文件→编辑→加密闭环，不重新实现加密逻辑。临时文件权限 MUST 为 600，编辑后 MUST 删除。

#### Scenario: vim 编辑触发挂起恢复
- **WHEN** 用户对 text/config 按 `e` 键
- **THEN** TUI 通过 `tea.ExecProcess` 挂起，vim 接管终端，vim 退出后 TUI 恢复并刷新列表

#### Scenario: 内容未改动不重新加密
- **WHEN** 用户在 vim 中未修改任何内容直接退出
- **THEN** 不触发重新加密保存（复用现有 "No changes detected" 逻辑），列表无变化

#### Scenario: 编辑失败提示
- **WHEN** vim 编辑过程出错（如编辑器不存在）
- **THEN** TUI 恢复后底部错误条显示具体错误，临时文件仍被清理

### Requirement: 错误处理与空状态

TUI SHALL 在操作出错时不崩溃，通过底部错误条展示错误信息。空分组/空列表 MUST 显示友好的空状态提示。

#### Scenario: 操作出错不崩溃
- **WHEN** 某 Manager 方法返回错误（如解密失败、key 不存在）
- **THEN** TUI 底部错误条显示错误信息，当前视图保留，不退出 TUI

#### Scenario: 空分组空状态
- **WHEN** 用户选中一个没有任何条目的分组
- **THEN** 列表区域显示空状态提示（如"该分组暂无环境变量"），不显示空白

#### Scenario: 错误条清除
- **WHEN** 错误条显示后用户执行下一次操作
- **THEN** 错误条被清除或在新操作成功后消失
