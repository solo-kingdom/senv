## MODIFIED Requirements

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
