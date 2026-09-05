## MODIFIED Requirements

### Requirement: 有效 session 时全入口复用

系统 SHALL 在所有需要解密的命令入口（`env`、`text`、`config`、`tui`、`interactive`）优先使用有效 session cache 中的 derived key，且 MUST NOT 再次提示密码。

#### Scenario: 有 restart session 时打开 TUI

- **WHEN** 用户已执行 `senv session start --timeout restart` 且 cache 有效，再运行 `senv tui`
- **THEN** 系统不提示密码并进入 TUI

#### Scenario: 有 session 时使用 config

- **WHEN** 用户存在有效 session，运行 `senv config list`（或其它 config 子命令）
- **THEN** 系统不提示密码并完成操作

#### Scenario: 有 session 时使用 interactive

- **WHEN** 用户存在有效 session，运行 `senv interactive`
- **THEN** 系统不提示密码并进入交互模式

### Requirement: session 缓存仅驻留平台安全存储

所有 timeout 模式（duration/restart）的 session 缓存 SHALL 仅驻留在平台验证的安全存储中：

- Linux SHALL 仅接受经操作系统确认的 memory-backed 文件系统（tmpfs/ramfs），并继续验证 `XDG_RUNTIME_DIR` 与任何 fallback 候选路径的实际 backing filesystem，不得仅依据环境变量名或 `/tmp` 路径推断其为 tmpfs。
- macOS SHALL 默认使用 Keychain（generic-password item，静态加密且随 keychain 锁定联动）存储 session cache。

系统 MUST 验证候选存储的实际安全属性。无法确认平台安全存储时 MUST fail closed、拒绝创建 session 且 MUST NOT 将派生钥写入持久盘；错误信息 MUST 给出可行动指引（平台推荐存储与逃生舱说明）。

系统 MAY 提供显式 opt-in 的磁盘逃生舱（用于 headless macOS、CI 等无平台安全存储的场景）。逃生舱默认关闭；开启时 MUST 输出醒目安全警告，且仍 MUST 保持 0600/0700 权限、独占/原子写入与 boot ID 校验。

系统 SHALL 在写缓存时清理历史遗留的持久化缓存文件。所有 timeout 模式的 session 均不承诺跨重启留存，`restart` 与重启失效语义在所有平台保持不变。

文件系统候选路径 SHALL 先经可信解析再校验组件：系统自带的符号链接（如 macOS `/var` → `/private/var`）MUST NOT 导致误拒；解析后的路径仍 MUST 无符号链接组件，写入 MUST 继续拒绝跟随目标及父路径中的符号链接。

#### Scenario: macOS 默认使用 Keychain

- **WHEN** 在 macOS 上执行 `senv session start` 且用户 login keychain 可用
- **THEN** session cache 写入 Keychain 的 senv generic-password item，后续命令无需重复输入密码，MCP 每请求读取不触发 Keychain 访问弹窗

#### Scenario: restart session 不再落盘用户数据目录

- **WHEN** 用户执行 `senv session start --timeout restart` 且平台安全存储可用
- **THEN** `~/.local/share/senv/session/` 下不存在缓存文件，缓存仅位于平台安全存储且文件权限满足存储介质的私有约束

#### Scenario: macOS Keychain 不可用 fail closed

- **WHEN** Keychain 锁定或不可用（如未解锁的 headless SSH 会话），且用户未显式开启磁盘逃生舱
- **THEN** `session start` 非零退出、不写入任何缓存，错误信息说明 Keychain 不可用并给出逃生舱开启方式

#### Scenario: restart session 在重启后失效

- **WHEN** restart 会话建立后系统重启，用户再次运行需要解密的命令
- **THEN** 缓存的 boot ID 校验失败，系统提示重新解锁，不产生解密失败误报

#### Scenario: 遗留持久缓存被清理

- **WHEN** 旧版本创建过持久缓存且用户以任意模式重新 start session
- **THEN** `~/.local/share/senv/session/` 下的遗留缓存文件被删除

#### Scenario: disk-backed XDG runtime 被拒绝

- **WHEN** Linux 上 `XDG_RUNTIME_DIR` 指向磁盘文件系统
- **THEN** `session start` fail closed、说明介质不安全，且不写入派生钥

#### Scenario: disk-backed fallback 被拒绝

- **WHEN** Linux 上 `XDG_RUNTIME_DIR` 为空且系统临时目录不是 memory-backed
- **THEN** 所有 timeout 模式均拒绝创建 cache，不回退到持久盘

#### Scenario: 系统符号链接不再误伤

- **WHEN** 候选 runtime 路径包含系统自带的符号链接（如 `/var/folders/...`）
- **THEN** 系统解析为真实路径后继续校验；若解析后介质合格则正常使用，若仍含符号链接或介质不合格则拒绝

#### Scenario: 显式 opt-in 磁盘逃生舱

- **WHEN** 用户显式开启磁盘逃生舱并执行 `session start`
- **THEN** 系统输出醒目安全警告，cache 以 0600 文件、0700 目录、原子写入与 boot ID 校验写入磁盘

#### Scenario: 逃生舱默认关闭

- **WHEN** 平台安全存储不可用且用户未显式开启磁盘逃生舱
- **THEN** 所有 timeout 模式均拒绝创建 cache，不写入任何磁盘文件

### Requirement: MCP 请求级 session 授权与撤销

已启动的 MCP server SHALL 在每个可能读取或修改 vault 的工具请求执行前，重新验证启动时授权对应的 session ID、到期时间、boot ID、data path、metadata salt 与 cached key。验证失败 MUST 在调用业务 manager 前拒绝请求、清除进程内 key，并返回要求重新启动 session/MCP server 的错误。启动后建立的新 session MUST NOT 自动授权持有旧 key 的 MCP 进程。

#### Scenario: duration session 到期
- **WHEN** MCP server 启动后原 session 超过 expiry，再调用任一 senv 工具
- **THEN** 请求在读取秘密或写入数据前失败，进程内 key 被清除

#### Scenario: session clear 主动撤销
- **WHEN** 用户在 MCP server 运行期间执行 `senv session clear`
- **THEN** 下一次工具请求失败，不返回旧 key 可解密的值，也不修改 vault

#### Scenario: session 被替换
- **WHEN** 用户重新执行 `session start` 生成不同 session ID
- **THEN** 旧 MCP server 的下一次请求被拒绝，必须重新启动后才能使用新授权

#### Scenario: restart session 保持有效
- **WHEN** memory-backed 的 restart session 未 clear、boot ID 未变化且 salt/key 仍匹配
- **THEN** MCP 请求继续成功，不因没有 expiry 而被误撤销

#### Scenario: metadata salt 改变
- **WHEN** rekey 或同步使 metadata salt 与 MCP 启动时授权不一致
- **THEN** 下一次请求被拒绝，旧 key 不再用于任何 manager 操作

## ADDED Requirements

### Requirement: 超时值校验

session 超时值 SHALL 仅接受 duration（`30m`/`8h`/`1d`/`1y` 等）与 `restart`。`never`、`infinite`、`forever` SHALL 被拒绝，错误信息 MUST 列出支持的取值。settings 中 `session.timeout` 为 `never` 时，`senv session start`（未显式传 `--timeout`）MUST 报错并给出可用值与修正指引。历史遗留 cache 的 `timeout_type` 为 `never` 时，系统 MUST 将其判为已过期并提示重新执行 `senv session start`，MUST NOT 崩溃或误报解密失败。

#### Scenario: 拒绝 never 超时值

- **WHEN** 用户执行 `senv session start --timeout never`（或 `infinite`/`forever`）
- **THEN** 命令报错，不写入 cache，错误信息列出支持的取值（duration 与 `restart`）

#### Scenario: settings 遗留 never 报错

- **WHEN** settings.json 配置 `"timeout": "never"` 且用户执行不带 `--timeout` 的 `senv session start`
- **THEN** 命令报错并提示更新 settings 为受支持的取值

#### Scenario: 遗留 never cache 判为过期

- **WHEN** 旧版本创建的 `timeout_type: "never"` cache 仍存在，用户运行 `senv session status` 或需要解密的命令
- **THEN** 系统报告 session 已过期并提示重新 `senv session start`，不误报为密码错误或数据损坏
