## ADDED Requirements

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
