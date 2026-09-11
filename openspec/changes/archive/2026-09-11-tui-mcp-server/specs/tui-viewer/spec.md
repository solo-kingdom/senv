## MODIFIED Requirements

### Requirement: Tab 切换

TUI SHALL 提供多个标签页（Env、Text、Config 及注入时注册的 SSH、AI、MCP、History、Audit）。用户 MUST 能通过 `Tab`/`Shift+Tab` 循环切换，并通过数字键 `1`–`9` 直达按注册顺序编号的 Tab。数字键 MUST 按已注册 Tab 数动态生效，越界数字 MUST 被忽略且不改变当前 Tab。每个 Tab MUST 有专属于该数据类型的布局和动作栏。

#### Scenario: 切换标签
- **WHEN** 用户在 Env Tab 按下 `Tab` 键或 `2` 键
- **THEN** 界面切换到 Text Tab，显示 text 分组与文本块列表

#### Scenario: 数字键直达全部 Tab
- **WHEN** 8 个 Tab 均已注册，用户按 `6`
- **THEN** 界面切换到 MCP Tab

#### Scenario: 越界数字不生效
- **WHEN** 仅注册 3 个 Tab（git 模式），用户按 `7`
- **THEN** 当前 Tab 不变，界面不报错

#### Scenario: 保留导航状态
- **WHEN** 用户从 Env Tab 切换到 Text Tab 再切回 Env Tab
- **THEN** Env Tab 恢复之前选中的分组和条目（导航状态不丢失）

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
