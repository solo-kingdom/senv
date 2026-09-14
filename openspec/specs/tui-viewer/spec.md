# tui-viewer Specification

## Purpose
TBD - created by archiving change add-tui-viewer. Update Purpose after archive.
## Requirements
### Requirement: TUI 启动命令

系统 SHALL 提供 `senv tui` 命令，启动全屏 TUI 界面浏览 env/text/config 数据。启动时 MUST 优先复用有效 session cache（derived key）；仅当无有效 session 时 MUST 提示密码。功能内密码认证 MUST NOT 写入或刷新 session cache。自动同步可用时，启动 MUST NOT 等待网络：界面 SHALL 先以本地工作副本（本地缓存数据）渲染，server 拉取在后台完成；后台拉取应用了远端变更时 SHALL 提示并以 stale-while-revalidate 更新界面：旧数据保持可见可操作，受影响 Tab 在后台完成单趟重载后静默替换，MUST NOT 清空为加载占位。

#### Scenario: 项目已初始化且 session 有效

- **WHEN** 用户运行 `senv tui`，项目已初始化且 session cache 有效
- **THEN** 系统不提示密码，进入全屏 TUI，默认显示 Env Tab 的 default 分组内容

#### Scenario: 项目已初始化、无 session 且密码正确

- **WHEN** 用户运行 `senv tui`，项目已初始化、无有效 session，输入正确密码
- **THEN** 系统进入全屏 TUI，默认显示 Env Tab 的 default 分组内容，且不创建 session cache

#### Scenario: 项目未初始化

- **WHEN** 用户运行 `senv tui` 但项目未初始化
- **THEN** 系统提示"项目未初始化，请先运行 senv init"并退出，不进入 TUI

#### Scenario: 密码错误

- **WHEN** 用户运行 `senv tui`，无有效 session 且输入错误密码
- **THEN** 系统提示"密码错误"并退出，不进入 TUI

#### Scenario: 启动不等待网络

- **WHEN** 用户运行 `senv tui`（server 模式），网络缓慢或不可达
- **THEN** 界面立即以本地缓存数据渲染，不阻塞在网络拉取上

#### Scenario: 后台拉取应用变更后更新展示

- **WHEN** 启动后的后台拉取从 server 应用了 N 条远端变更
- **THEN** 界面提示「已从 server 更新 N 条」；旧列表保持可见可操作，重载完成后条目静默更新，光标与过滤条件不丢失

#### Scenario: `--refresh` 绕过节流但不阻塞

- **WHEN** 用户运行 `senv tui --refresh`
- **THEN** 启动后台拉取绕过节流窗口强制执行，界面同样先以本地数据渲染


### Requirement: Tab 切换

TUI SHALL 提供多个标签页（Env、Text、Config 及注入时注册的 SSH、KeyPair、AI、MCP、History、Audit）。用户 MUST 能通过 `Tab`/`Shift+Tab` 循环切换，并通过数字键 `1`–`9` 直达按注册顺序编号的 Tab。数字键 MUST 按已注册 Tab 数动态生效，越界数字 MUST 被忽略且不改变当前 Tab。每个 Tab MUST 有专属于该数据类型的布局和动作栏。

#### Scenario: 切换标签
- **WHEN** 用户在 Env Tab 按下 `Tab` 键或 `2` 键
- **THEN** 界面切换到 Text Tab，显示 text 分组与文本块列表

#### Scenario: 数字键直达全部 Tab
- **WHEN** 全部 Tab 已注册（注册顺序 Env、Text、Config、SSH、KeyPair、AI、MCP、History、Audit），用户按 `6`
- **THEN** 界面切换到 AI Tab

#### Scenario: 越界数字不生效
- **WHEN** 仅注册 4 个 Tab，用户按 `7`
- **THEN** 当前 Tab 不变，界面不报错

#### Scenario: 保留导航状态
- **WHEN** 用户从 Env Tab 切换到 Text Tab 再切回 Env Tab
- **THEN** Env Tab 恢复之前选中的分组和条目（导航状态不丢失）

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

每个 Tab SHALL 支持按 `/` 键触发当前 Tab 内的过滤，仅匹配 key/name 等标识字段（不匹配值），匹配大小写不敏感。单栏 Tab 过滤作用于其主列表；SSH/AI/MCP 双栏 Tab 过滤作用于左栏主列表，右栏 SHALL 随左栏当前选中项联动；Config Tab 的过滤与侧栏计数行为见 config-tui 能力规约；Audit Tab 的预设过滤快捷键 SHALL 保留并与自由文本过滤叠加。`esc` SHALL 清除过滤并恢复完整列表。

#### Scenario: 过滤当前列表
- **WHEN** 用户在 Env Tab 按 `/` 键并输入 `DATABASE`
- **THEN** 右侧列表仅显示 key 含 `DATABASE` 的环境变量（忽略大小写）

#### Scenario: 双栏 Tab 过滤主列表
- **WHEN** 用户在 SSH Tab 按 `/` 键并输入 `web`
- **THEN** 左栏仅显示别名或 hostname 含 `web` 的 host（忽略大小写），右栏显示当前选中 host 的 keypair 联动信息

#### Scenario: 清除过滤
- **WHEN** 用户清空过滤输入框或按 `esc`
- **THEN** 列表恢复显示全部分组内的条目

### Requirement: 全局跨类型搜索

TUI SHALL 提供全局搜索 overlay（触发键 `S`），跨 Env/Text/Config/SSH/AI/MCP 数据搜索。搜索 MUST 只匹配标识字段（key/name、host alias/hostname、provider alias、MCP 档案 alias/command），绝不匹配值、私钥内容、凭据或 MCP env 值。搜索结果 MUST 标识条目类型，并支持跳转定位（SSH/AI/MCP 结果跳转到对应 Tab 并定位光标）。

#### Scenario: 触发全局搜索
- **WHEN** 用户按 `S` 键
- **THEN** 弹出全局搜索 overlay，含输入框和跨类型结果列表

#### Scenario: 搜索结果按类型展示
- **WHEN** 用户输入 `database` 且数据中存在匹配的 env key、text key、config name
- **THEN** 结果列表显示所有匹配项，每项标注类型（Env/Text/Cfg）、分组（若适用）、key/name，值部分遮蔽（env 显示 `***`，text 显示 size，config 显示 target）

#### Scenario: SSH 与 AI 结果
- **WHEN** 用户输入某 host alias 或 provider alias 的前缀
- **THEN** 结果显示对应 SSH host 或 LLM provider 条目，`enter` 后跳转到对应 Tab 并选中该条目

#### Scenario: MCP 档案结果
- **WHEN** 用户输入某 MCP Server 档案 alias 或 command 的前缀
- **THEN** 结果显示对应 MCP 条目，`enter` 后跳转到 MCP Tab 并选中该档案

#### Scenario: 搜索不匹配值
- **WHEN** 用户输入某个仅出现在值中而不在任何 key/name 中的字符串
- **THEN** 结果列表为空（显示"无匹配"），不返回任何值匹配

#### Scenario: 搜索不返回秘密
- **WHEN** 用户输入某个仅出现在 SSH 私钥内容、LLM 凭据或 MCP env 值中的字符串
- **THEN** 结果列表为空（显示"无匹配"）

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

### Requirement: TUI 视觉外框与 Tab 可辨识性

TUI SHALL 以一圈连续的边框字符包裹整个界面（顶、底、左、右四边均可见），形成单一应用窗口边界。三个标签页（Env/Text/Config）SHALL 在顶部边框内渲染为一行 tab 栏，其中当前激活的 tab MUST 通过背景色块与其它 tab 明显区分，tab 之间 SHALL 有可见分隔符。底部状态栏与错误栏 SHALL 渲染在外框内部。当终端尺寸不足以容纳完整布局时，系统 SHALL 显示最小尺寸提示而非渲染错乱的 chrome。

#### Scenario: 整个界面被外框包裹

- **WHEN** 用户在已初始化项目运行 `senv tui` 并通过密码校验，终端尺寸为正常可用尺寸（如 80×24）
- **THEN** 渲染输出的首行与末行 SHALL 包含 box-drawing 边框字符（圆角 `╭`/`╮`/`╰`/`╯` 或退化后的 ASCII 等价物），输出的最左与最右列 SHALL 为竖边框字符，且边框连续无缺口

#### Scenario: 激活 tab 视觉上明显区分于非激活 tab

- **WHEN** TUI 渲染 tab 栏，当前激活的是 Env tab（默认状态）
- **THEN** "Env" 标签 SHALL 以背景色块渲染（lipgloss background ANSI 转义序列包裹），"Text" 与 "Config" 标签 SHALL 不带背景色块，使得激活 tab 在视觉上像凸起的 tab 而非普通文字

#### Scenario: tab 之间存在分隔符

- **WHEN** TUI 渲染 tab 栏
- **THEN** 相邻两个 tab 标签之间 SHALL 存在可见分隔字符（`│` 或等价竖线），三个 tab 共产生两个分隔符

#### Scenario: 底部状态栏位于外框内部

- **WHEN** TUI 渲染正常状态（无错误）且终端底部显示状态栏的帮助快捷键提示
- **THEN** 状态栏行 SHALL 位于底部边框行的上一行（即外框内部最后一行），状态栏文本左侧 SHALL 不超出左竖边框

#### Scenario: 错误栏同样位于外框内部

- **WHEN** 某次 Manager 操作返回错误，错误栏被触发显示
- **THEN** 错误栏 SHALL 渲染在状态栏同一位置（外框内部底行），错误文本被截断以不溢出右竖边框，底部边框行保持完整

#### Scenario: 终端尺寸过小时显示最小尺寸提示

- **WHEN** 终端尺寸小于最小阈值（高度 < 6 行 或 宽度 < 30 列）
- **THEN** TUI SHALL 不渲染 tab 栏、内容区与外框 chrome，而是显示居中的「terminal too small」提示文本，避免布局错乱

#### Scenario: 内容区不溢出外框

- **WHEN** 终端尺寸为正常可用尺寸，任意 tab（Env/Text/Config）渲染其内容
- **THEN** 内容区 SHALL 完全位于外框内部（不跨越左右竖边框、不超出底部边框），切换 tab 时不出现字符错位或滚动条溢出

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

#### Scenario: 多选键全部列出
- **WHEN** 用户在 Env/Text/Config/SSH/MCP 列表 Tab 打开键位总览
- **THEN** 总览与底栏列出 `space`（勾选/取消）与 `a`（全选当前过滤可见集），与这些 Tab 分发处理的多选按键一致

#### Scenario: 确认态强制键列出
- **WHEN** 用户在 KeyPair Tab 对仍被引用的 keypair 按 `d` 进入删除确认页后打开键位总览
- **THEN** 总览列出 `F`（强制删除并清除 host 引用），与确认页内提示的按键一致

### Requirement: 面板内容截断与详情

TUI 的所有面板内容 MUST 不依赖 lipgloss `Width` 换行：列表行与详情行 SHALL 在面板宽度内截断（超长以 `…` 结尾），完整内容 SHALL 通过 `enter` 打开的详情弹层查看。任何面板 MUST NOT 因长值（base_url、模型列表、hostname、路径）而撑高或折行。

#### Scenario: 长值截断
- **WHEN** provider 的模型列表或 base_url 超过所在面板宽度
- **THEN** 该行在面板内截断显示，面板高度与行数不变

#### Scenario: 详情弹层看全文
- **WHEN** 用户在列表上按 `enter`
- **THEN** 弹出详情层展示未截断的完整字段，`esc` 关闭并回到列表

### Requirement: 同步状态可见性

当自动同步可用（server provider 且未关闭 auto_sync）时，TUI SHALL 在底部常驻显示待推送条数与最近一次同步结果；写操作完成后 SHALL 异步触发 push（沿用 2 秒预算）。启动时 SHALL 在后台异步触发一次拉取（沿用 2 秒预算；`--refresh` 绕过节流窗口）：应用了远端变更（条目或 metadata）时 SHALL 给出成功提示并以 stale-while-revalidate 更新各标签（旧数据保持可见可操作，MUST NOT 清空为加载占位）；无变更或零网络跳过（节流/锁忙）时 MUST NOT 出现成功提示，仅更新状态条。拉取失败（含 client 被屏蔽）SHALL 在界面内提示原因且 MUST NOT 退出进程。退出 TUI 前若仍有待推送条目，TUI SHALL 在界面内给出一次提示。自动同步不可用时 MUST NOT 显示该状态，也不得触发拉取或阻止任何操作。

#### Scenario: 显示待推送状态

- **WHEN** 用户在 server 模式修改一条 env 且 push 成功
- **THEN** 底部状态从「N 条待推送」变为已同步状态

#### Scenario: push 失败不丢数据

- **WHEN** 写操作已本地落盘但 push 失败
- **THEN** TUI 内显示待推送条数与失败原因，本地数据保持已写入状态

#### Scenario: 退出前提示

- **WHEN** 用户按 `q` 退出且仍有待推送条目
- **THEN** 界面先显示一次待推送提示，再退出

#### Scenario: 非 server 模式静默

- **WHEN** 项目使用 git provider
- **THEN** 底部不显示同步状态，界面与操作不受影响，也不发起后台拉取

#### Scenario: 后台拉取无变更不出提示

- **WHEN** 启动后台拉取时远端无新变更，或处于节流窗口/同步锁忙而零网络跳过
- **THEN** 不出现成功提示，底部状态条仅反映最近同步时间，各 Tab 不重载

#### Scenario: 拉取失败不退出

- **WHEN** 启动后台拉取失败（网络错误或 client 被屏蔽）
- **THEN** 错误栏显示原因（被屏蔽含重新注册指引），TUI 保持可用，本地数据不受影响

#### Scenario: 拉取应用变更不清空列表

- **WHEN** 用户正浏览 env 列表时后台 pull 应用了 3 条远端变更
- **THEN** 列表保持可见可操作，重载完成后条目静默更新，光标与过滤条件不丢失

### Requirement: History Tab 延迟加载

TUI 启动 SHALL NOT 发起 History 查询；History 数据 SHALL 在用户首次激活 History Tab 时查询并缓存。激活后的刷新语义保持既有行为（server 模式提供、手动刷新可用；git 模式或 server 不可用时优雅降级为无数据/空态）。

#### Scenario: 启动不查 History

- **WHEN** server 模式下启动 TUI 且用户停留在 env Tab
- **THEN** 进程未发起任何 History 请求，其余 Tab 行为不变

#### Scenario: 首次激活 History Tab

- **WHEN** 用户首次切换到 History Tab
- **THEN** 发起一次查询并在加载完成后展示；再次激活时复用缓存，手动刷新才重新查询

### Requirement: env 数据单趟加载与共享快照

TUI 对 vault 数据的全量消费 SHALL 通过单趟加载构建的内存快照完成：一次遍历读取每个条目的密文文件至多一次。env Tab 列表、全局搜索、AI Tab 凭据引用收集等消费方 SHALL 复用同一份快照，MUST NOT 各自重复全量遍历。写操作成功后快照 SHALL 失效并在后台单趟重建；单条读写路径（如 `senv env get`）行为不变。

#### Scenario: 启动只读每个条目一次

- **WHEN** server 模式暖启动 TUI 且远端无变更
- **THEN** env/text 等 vault 条目文件在启动装载过程中各被读取一次（以耗时日志的条目数/次数维度可验证），列表可用

#### Scenario: 写操作后快照单趟重建

- **WHEN** 用户在 TUI 内修改一个环境变量
- **THEN** 快照失效并单趟重建，期间 UI 不清空、其余条目不再重复读取

### Requirement: 多选集与批量操作

TUI 列表（Env 变量、Text 文本块、SSH host/keypair、Config 条目、MCP 档案）SHALL 支持 `space` 勾选/取消勾选光标条目形成多选集；`a` SHALL 全选当前过滤可见集，再按一次取消全选可见集。多选集 SHALL 跨过滤条件变化持久，被过滤隐藏的已选项 MUST 保持选中，状态栏 SHALL 提示已选总数与被过滤隐藏数（无勾选时不显示）。批量安全写动词（删除、导出、安装/卸载、导出/撤回）在多选集非空时 SHALL 作用于多选集，为空时 SHALL 回落为游标单条（纯单选行为不变）；批量执行 SHALL 复用既有确认流（计划预览、逐条确认或删除二次确认），MUST NOT 出现免确认批量写，单条失败 MUST NOT 中止其余并在结束时汇总。单实体操作（编辑、重命名、meta、详情）仅在选择数 ≤1 时可用。多选集 MUST NOT 跨栏（双栏 Tab 的侧栏/次栏不参与勾选）；批量操作提交后 SHALL 清空多选集。AI Tab 切换向导内的模型勾选保持自有流程，MUST NOT 与列表多选集混淆。

#### Scenario: 勾选并批量删除
- **WHEN** 用户在 Env Tab 对 3 个变量按 `space` 勾选后按 `d` 并确认
- **THEN** 一次确认列出 3 个目标，确认后 3 条全部删除，选择集清空

#### Scenario: 全选过滤可见集
- **WHEN** 用户以 `web` 过滤后按 `a`
- **THEN** 仅当前可见的匹配条目入选多选集，不匹配的未过滤条目不在集内

#### Scenario: 选择集跨过滤持久
- **WHEN** 用户勾选 2 条后按 `esc` 清除过滤
- **THEN** 这 2 条保持选中，状态栏提示已选 2；被过滤隐藏的已选项在清过滤后仍为选中态

#### Scenario: 空选择集回落游标
- **WHEN** 用户未勾选任何条目按 `d`
- **THEN** 仅游标所在条目进入删除确认，行为与引入多选前一致

#### Scenario: 单实体操作受限
- **WHEN** 用户勾选 ≥2 条后按 `e`
- **THEN** 界面提示需先缩小到单选（取消多余勾选或清空选择集），不打开编辑

#### Scenario: 批量导出需确认
- **WHEN** 用户在 SSH Tab 勾选 3 个 host 按 `x`
- **THEN** 确认页列出将生成的全部目标路径，确认后逐条导出，单条失败不中止其余并汇总结果

#### Scenario: AI 向导不混淆
- **WHEN** 用户在 AI 切换向导的模型集步骤按 `space`
- **THEN** 勾选的是向导内的候选模型，与列表多选集无关

### Requirement: Tab 加载态

TUI 的每个 Tab SHALL 区分加载态与空态：数据装载完成前，Tab SHALL 在常驻面板几何内显示加载提示（与既有 Tab 的 `loading groups…` 同风格、同界面语言），MUST NOT 显示携带操作指引的空态文案（如 "no LLM provider profiles yet; … press n to create"）；空态文案与操作指引 SHALL 仅在装载完成后、数据集确实为空时出现。加载提示的呈现方式与 env/text/config 既有范式一致。错误态（装载失败）SHALL 展示失败原因，与加载态、空态区分。

#### Scenario: AI Tab 装载期间显示加载态

- **WHEN** 用户切换到 AI Tab 且 provider 数据尚未装载完成
- **THEN** 内容区渲染常驻面板几何，框内显示加载提示，不出现 "no LLM provider profiles yet" 等空态指引

#### Scenario: MCP Tab 装载期间显示加载态

- **WHEN** 用户切换到 MCP Tab 且 server 档案数据尚未装载完成
- **THEN** 内容区渲染常驻面板几何，框内显示加载提示，不出现 "no MCP server profiles yet" 等空态指引

#### Scenario: SSH Tab 装载期间显示加载态

- **WHEN** 用户切换到 SSH Tab 且 host/keypair 数据尚未装载完成
- **THEN** 内容区渲染常驻面板几何，框内显示加载提示，不出现 "no SSH assets yet" 等空态指引

#### Scenario: 装载完成后空态恢复指引

- **WHEN** AI/MCP/SSH Tab 数据装载完成且数据集为空
- **THEN** 显示既有空态文案与操作指引（如 "press n to create"），布局与装载期间一致、无跳动

#### Scenario: History Tab 装载期间显示加载态

- **WHEN** 用户首次激活 History Tab、查询尚未返回
- **THEN** 内容区渲染常驻面板几何并在框内显示加载提示，而非无框裸文本

#### Scenario: Audit Tab 装载期间显示加载态

- **WHEN** Audit Tab 数据尚未装载完成
- **THEN** 内容区渲染常驻面板几何并在框内显示加载提示，而非无框裸文本

### Requirement: History 与 Audit 面板几何

History Tab 与 Audit Tab 的内容面板 SHALL 撑满内容区可用宽高（与其他 Tab 的外层几何一致），并在终端尺寸变化时跟随重排。列表 SHALL 采用与其他 Tab 一致的窗口化呈现：可见行数受面板高度约束、光标始终可见，列表超出可见范围时标题 SHALL 显示当前可见区间（如「4–12」）；行内容超宽时 SHALL 以 `…` 截断，MUST NOT 撑破外框。空态、错误态与恢复确认等附加信息 SHALL 呈现在面板内部。

#### Scenario: 面板撑满内容区

- **WHEN** 终端为正常可用尺寸（如 80×24），用户切换到 History Tab 或 Audit Tab
- **THEN** 面板边框占满内容区宽高，与其他 Tab 的面板外框视觉一致，不随行数或行宽伸缩

#### Scenario: resize 跟随重排

- **WHEN** 用户在 History Tab 或 Audit Tab 停留时调整终端宽度或高度
- **THEN** 面板边框跟随新尺寸重排，与其他 Tab 行为一致

#### Scenario: 列表窗口化与区间提示

- **WHEN** History Tab 版本数超过面板可视行数且光标移出当前窗口
- **THEN** 列表滚动跟随光标，标题显示当前可见区间（如「4–12」）

#### Scenario: 附加信息位于面板内部

- **WHEN** History Tab 进入恢复确认或显示 flash 提示，或 Audit Tab 显示过滤输入行
- **THEN** 该信息渲染在面板边框内部，面板总高不超出内容区

