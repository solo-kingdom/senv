## MODIFIED Requirements

### Requirement: TUI 启动命令

系统 SHALL 提供 `senv tui` 命令，启动全屏 TUI 界面浏览 env/text/config 数据。启动时 MUST 优先复用有效 session cache（derived key）；仅当无有效 session 时 MUST 提示密码。功能内密码认证 MUST NOT 写入或刷新 session cache。自动同步可用时，启动 MUST NOT 等待网络：界面 SHALL 先以本地工作副本（本地缓存数据）渲染，server 拉取在后台完成；后台拉取应用了远端变更时 SHALL 提示并更新界面数据。

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
- **THEN** 界面提示「已从 server 更新 N 条」，各标签数据随后更新为拉取后的本地工作副本

#### Scenario: `--refresh` 绕过节流但不阻塞

- **WHEN** 用户运行 `senv tui --refresh`
- **THEN** 启动后台拉取绕过节流窗口强制执行，界面同样先以本地数据渲染

### Requirement: 同步状态可见性

当自动同步可用（server provider 且未关闭 auto_sync）时，TUI SHALL 在底部常驻显示待推送条数与最近一次同步结果；写操作完成后 SHALL 异步触发 push（沿用 2 秒预算）。启动时 SHALL 在后台异步触发一次拉取（沿用 2 秒预算；`--refresh` 绕过节流窗口）：应用了远端变更（条目或 metadata）时 SHALL 给出成功提示并重载各标签的本地数据；无变更或零网络跳过（节流/锁忙）时 MUST NOT 出现成功提示，仅更新状态条。拉取失败（含 client 被屏蔽）SHALL 在界面内提示原因且 MUST NOT 退出进程。退出 TUI 前若仍有待推送条目，TUI SHALL 在界面内给出一次提示。自动同步不可用时 MUST NOT 显示该状态，也不得触发拉取或阻止任何操作。

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
- **THEN** 不出现成功提示，底部状态条仅反映最近同步时间

#### Scenario: 拉取失败不退出

- **WHEN** 启动后台拉取失败（网络错误或 client 被屏蔽）
- **THEN** 错误栏显示原因（被屏蔽含重新注册指引），TUI 保持可用，本地数据不受影响
