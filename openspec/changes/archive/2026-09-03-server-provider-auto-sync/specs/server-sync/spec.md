## ADDED Requirements

### Requirement: 读路径自动拉取

server provider 启用自动同步时，读取类命令（env 导出、list、text 读、session、TUI 打开）SHALL 在返回数据前按节流窗口执行 best-effort 增量拉取：距上次成功拉取超过窗口且 server 可达时，先落盘远端更改再返回；否则直接返回本地缓存。拉取 MUST 有超时预算，失败（超时/不可达）MUST 静默降级为本地缓存，MUST NOT 使命令失败或显著增加延迟。本地有未推送更改的条目 MUST NOT 被远端版本覆盖。

#### Scenario: 节流窗口内不产生网络请求

- **WHEN** 距上次成功拉取不足节流窗口时执行读命令
- **THEN** 命令直接使用本地缓存返回，不发起网络请求

#### Scenario: 窗口过期且 server 可达

- **WHEN** 超过节流窗口后执行读命令且 server 可达
- **THEN** 远端更改先落盘并提示更新条数，命令返回的数据为最新内容

#### Scenario: 拉取超时或不可达

- **WHEN** 拉取超过超时预算或 server 不可达时执行读命令
- **THEN** 命令正常返回本地缓存数据，无报错、无可感知额外延迟

### Requirement: 写路径自动推送

server provider 启用自动同步时，写入类命令 SHALL 先完成本地落盘并立即返回结果，随后在进程退出前 best-effort 推送本地待推送更改。推送失败（网络不可达或乐观锁冲突）MUST NOT 使命令失败，MUST 保留待推送状态由后续命令自动重试，且 SHALL 输出一行警告说明待推送条目数与解决方式（冲突时指引 `senv sync`）。

#### Scenario: 写入后无感推送成功

- **WHEN** server 可达时执行写命令
- **THEN** 本地写入成功、更改自动推送，命令输出正常结果且无错误

#### Scenario: 推送时 server 不可达

- **WHEN** 写命令完成本地落盘但推送时 server 不可达
- **THEN** 命令成功返回，输出待推送警告，本地待推送状态保留

#### Scenario: 推送遇到冲突

- **WHEN** 写命令的推送因远端已更新而返回冲突
- **THEN** 命令成功返回本地写入结果，输出冲突警告并列出冲突条目与 `senv sync` 解决指引，两端数据均不被自动覆盖

### Requirement: 关键写阻塞推送

低频关键写操作（修改 vault 口令、初始化后的首次写入）SHALL 在命令内以阻塞方式推送并在返回前确认结果。推送失败时命令 MUST 明确告警（说明其他设备在同步前无法获得此次更改）并指引手动执行 `senv sync`，本地更改保持已生效。

#### Scenario: 修改口令后推送失败

- **WHEN** 修改 vault 口令时 server 不可达
- **THEN** 口令在本地生效，命令输出明确的推送失败告警与 `senv sync` 指引

### Requirement: 并发同步串行化

同一缓存目录上的并发命令执行时，系统 SHALL 对同步段（拉取、推送及同步状态更新）做进程间互斥，MUST NOT 因并发导致同步状态损坏或重复推送同一更改。

#### Scenario: 两个进程同时执行写命令

- **WHEN** 同一台机器上两个 senv 写命令并发执行且都触发推送
- **THEN** 同步段串行执行，同步状态一致，无条目丢失或状态损坏

### Requirement: 自动同步开关与逃生口

settings SHALL 支持按 vault 配置 `auto_sync`（server provider 下默认开启）。关闭时读写命令 MUST NOT 触发任何网络请求，行为回到纯手动 `senv sync` 模式。读命令 SHALL 支持 `--refresh` 绕过节流窗口强制拉取。

#### Scenario: 关闭自动同步

- **WHEN** `auto_sync` 配置为关闭时执行读写命令
- **THEN** 命令不产生任何网络请求，同步仅通过手动 `senv sync` 完成

#### Scenario: 强制刷新

- **WHEN** 在节流窗口内以 `--refresh` 执行读命令且 server 可达
- **THEN** 命令绕过节流窗口执行拉取并返回最新数据
