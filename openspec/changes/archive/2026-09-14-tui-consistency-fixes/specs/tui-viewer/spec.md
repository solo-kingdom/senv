## MODIFIED Requirements

### Requirement: 键位总览

TUI SHALL 提供键位总览 overlay（触发键 `?`），列出全局键位与当前 Tab 的键位，`?` 或 `esc` SHALL 关闭它。文本输入模式（`InputMode`）下 `?` MUST 作为普通字符输入而不触发 overlay。键位总览与状态栏提示 SHALL 由中央 keymap 注册表生成，列出的键位与说明 MUST 和实际行为一致；反向亦真：每个 Tab 在按键分发中实际处理的每个按键（含浏览态与确认/计划页等输入模式态，如多选 `space`/`a`、KeyPair 删除确认的 `F`）MUST 全部注册进该注册表，`?` 总览与底栏 MUST NOT 遗漏任何实际可用的按键。同一动作在不同 Tab MUST 使用相同按键：重命名统一为 `r`，刷新统一为 `Ctrl+R`，确认统一为 `y`/`enter`、取消统一为 `esc`/`n`（计划确认页中除 `esc`/`n`/`y`/`enter`/`F` 外的按键 MUST 被忽略，MUST NOT 被解释为取消或放行）。

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
