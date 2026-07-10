## MODIFIED Requirements

### Requirement: TUI 启动命令

系统 SHALL 提供 `senv tui` 命令，启动全屏 TUI 界面浏览 env/text/config 数据。启动时 MUST 优先复用有效 session cache（derived key）；仅当无有效 session 时 MUST 提示密码。功能内密码认证 MUST NOT 写入或刷新 session cache。

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
