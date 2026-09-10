# tui-viewer Delta

## MODIFIED Requirements

### Requirement: Tab 切换

TUI SHALL 提供多个标签页（Env、Text、Config 及注入时注册的 SSH、AI、History、Audit）。用户 MUST 能通过 `Tab`/`Shift+Tab` 循环切换，并通过数字键 `1`–`9` 直达按注册顺序编号的 Tab。数字键 MUST 按已注册 Tab 数动态生效，越界数字 MUST 被忽略且不改变当前 Tab。每个 Tab MUST 有专属于该数据类型的布局和动作栏。

#### Scenario: 切换标签
- **WHEN** 用户在 Env Tab 按下 `Tab` 键或 `2` 键
- **THEN** 界面切换到 Text Tab，显示 text 分组与文本块列表

#### Scenario: 数字键直达全部 Tab
- **WHEN** 7 个 Tab 均已注册，用户按 `6`
- **THEN** 界面切换到 History Tab

#### Scenario: 越界数字不生效
- **WHEN** 仅注册 3 个 Tab（git 模式），用户按 `7`
- **THEN** 当前 Tab 不变，界面不报错

#### Scenario: 保留导航状态
- **WHEN** 用户从 Env Tab 切换到 Text Tab 再切回 Env Tab
- **THEN** Env Tab 恢复之前选中的分组和条目（导航状态不丢失）

### Requirement: 全局跨类型搜索

TUI SHALL 提供全局搜索 overlay（触发键 `S`），跨 Env/Text/Config/SSH/AI 数据搜索。搜索 MUST 只匹配标识字段（key/name、host alias/hostname、provider alias），绝不匹配值、私钥内容或凭据。搜索结果 MUST 标识条目类型，并支持跳转定位（SSH/AI 结果跳转到对应 Tab 并定位光标）。

#### Scenario: 触发全局搜索
- **WHEN** 用户按 `S` 键
- **THEN** 弹出全局搜索 overlay，含输入框和跨类型结果列表

#### Scenario: 搜索结果按类型展示
- **WHEN** 用户输入 `database` 且数据中存在匹配的 env key、text key、config name
- **THEN** 结果列表显示所有匹配项，每项标注类型（Env/Text/Cfg）、分组（若适用）、key/name，值部分遮蔽（env 显示 `***`，text 显示 size，config 显示 target）

#### Scenario: SSH 与 AI 结果
- **WHEN** 用户输入某 host alias 或 provider alias 的前缀
- **THEN** 结果显示对应 SSH host 或 LLM provider 条目，`enter` 后跳转到对应 Tab 并选中该条目

#### Scenario: 搜索不匹配值
- **WHEN** 用户输入某个仅出现在值中而不在任何 key/name 中的字符串
- **THEN** 结果列表为空（显示"无匹配"），不返回任何值匹配

#### Scenario: 搜索不返回秘密
- **WHEN** 用户输入某个仅出现在 SSH 私钥内容或 LLM 凭据中的字符串
- **THEN** 结果列表为空（显示"无匹配"）

#### Scenario: 跳转定位
- **WHEN** 用户在搜索结果选中某条按 `enter`
- **THEN** overlay 关闭，切换到该条目所属的 Tab，选中对应分组（若有）和条目

#### Scenario: 关闭搜索
- **WHEN** 用户按 `esc`
- **THEN** overlay 关闭，返回之前的 Tab 视图

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
- **THEN** 显示空状态提示说明如何创建（如"执行 senv host add 添加后按 r 刷新"），不显示空白面板

#### Scenario: 错误条清除
- **WHEN** 错误条显示后用户执行下一次操作
- **THEN** 错误条被清除或在新操作成功后消失

## ADDED Requirements

### Requirement: 键位总览

TUI SHALL 提供键位总览 overlay（触发键 `?`），列出全局键位与当前 Tab 的键位，`?` 或 `esc` SHALL 关闭它。文本输入模式（`InputMode`）下 `?` MUST 作为普通字符输入而不触发 overlay。

#### Scenario: 打开键位总览
- **WHEN** 用户按 `?`
- **THEN** 弹出 overlay，分「全局」与当前 Tab 两组列出键位与说明

#### Scenario: 输入模式不劫持
- **WHEN** 用户正在表单/过滤输入框中输入 `?`
- **THEN** `?` 作为字符进入输入框，不打开 overlay

### Requirement: 面板内容截断与详情

TUI 的所有面板内容 MUST 不依赖 lipgloss `Width` 换行：列表行与详情行 SHALL 在面板宽度内截断（超长以 `…` 结尾），完整内容 SHALL 通过 `enter` 打开的详情弹层查看。任何面板 MUST NOT 因长值（base_url、模型列表、hostname、路径）而撑高或折行。

#### Scenario: 长值截断
- **WHEN** provider 的模型列表或 base_url 超过所在面板宽度
- **THEN** 该行在面板内截断显示，面板高度与行数不变

#### Scenario: 详情弹层看全文
- **WHEN** 用户在列表上按 `enter`
- **THEN** 弹出详情层展示未截断的完整字段，`esc` 关闭并回到列表

### Requirement: 同步状态可见性

当自动同步可用（server provider 且未关闭 auto_sync）时，TUI SHALL 在底部常驻显示待推送条数与最近一次同步结果；写操作完成后 SHALL 异步触发 push（沿用 2 秒预算）。退出 TUI 前若仍有待推送条目，TUI SHALL 在界面内给出一次提示。自动同步不可用时 MUST NOT 显示该状态，也不得阻止任何操作。

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
- **THEN** 底部不显示同步状态，界面与操作不受影响
