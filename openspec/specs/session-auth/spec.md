# session-auth Specification

## Purpose

统一 senv 各命令入口的 session 认证契约：有有效 session 则复用 derived key；无 session 时功能内密码仅作临时认证；仅 `senv session start` 写入 session cache。
## Requirements
### Requirement: 有效 session 时全入口复用

系统 SHALL 在所有需要解密的命令入口（`env`、`text`、`config`、`tui`、`interactive`）优先使用有效 session cache 中的 derived key，且 MUST NOT 再次提示密码。

#### Scenario: 有 never session 时打开 TUI

- **WHEN** 用户已执行 `senv session start --timeout never` 且 cache 有效，再运行 `senv tui`
- **THEN** 系统不提示密码并进入 TUI

#### Scenario: 有 session 时使用 config

- **WHEN** 用户存在有效 session，运行 `senv config list`（或其它 config 子命令）
- **THEN** 系统不提示密码并完成操作

#### Scenario: 有 session 时使用 interactive

- **WHEN** 用户存在有效 session，运行 `senv interactive`
- **THEN** 系统不提示密码并进入交互模式

### Requirement: 功能内密码仅临时认证

当不存在有效 session 时，系统 MAY 在**交互式**功能命令中提示密码以完成当次操作；该密码认证 MUST NOT 创建或刷新 session cache。仅 `senv session start` SHALL 写入或更新 session cache。同一进程内首次密码成功后，后续入口 MUST 按「单次进程内鉴权复用」复用结果，MUST NOT 再次提示。非交互或被捕获的 `env export` 路径 MUST 遵守「禁止弹密码并提示 session start」的要求，不得以多次临时密码代替 session。

#### Scenario: 无 session 时 env 要密码但不落盘

- **WHEN** 无有效 session，用户在交互式终端运行 `senv env get FOO` 并输入正确密码
- **THEN** 命令成功返回值，且随后 `senv session status` 仍显示无 active session

#### Scenario: 无 session 时 TUI 要密码但不落盘

- **WHEN** 无有效 session，用户运行 `senv tui` 并输入正确密码
- **THEN** 系统进入 TUI，且不调用 session 写入；退出后 `senv session status` 仍无 active session

#### Scenario: 仅 session start 写入 cache

- **WHEN** 用户运行 `senv session start --timeout 8h` 并输入正确密码
- **THEN** 系统写入 session cache，`senv session status` 显示 Active

#### Scenario: 交互临时密码在同进程内只问一次

- **WHEN** 无有效 session，用户在交互式终端运行会触发 env 与 text 双重解析的命令（如含引用的 `env export`）并输入一次正确密码
- **THEN** 全程密码提示至多一次，命令成功，且不写入 session cache

### Requirement: config 支持 derived key

`config.Manager` 与 storage 的 config 读写 SHALL 支持使用 session 提供的 derived key（与 env/text 同构），以便在有效 session 下无需密码即可加解密配置文件。

#### Scenario: 用 key 读写 config

- **WHEN** 调用方使用 derived key 构造 `config.Manager` 并 load/save 某配置
- **THEN** 加解密成功，行为与使用正确密码时一致

### Requirement: stale session 的诊断不得误报为密码错误

当 session cache 与当前 `metadata.json` 不一致（`cache.Salt != metadata.Salt`，或 cached key 无法解开 `metadata.PasswordKey`）时，系统 SHALL 将其归类为会话失效（stale），MUST NOT 把后续密码校验的失败笼统报告为 `invalid password`。当诊断表明 `metadata` 与加密数据文件不同步时，系统 SHALL 向用户报告 `ErrDataDesync` 及真实原因（如"metadata 与数据文件不是同一套密钥"），并给出恢复指引。

#### Scenario: desync 时报告真实原因而非密码错

- **WHEN** session cache 的 key 能解开 `env_*.json.enc` 但解不开 `metadata.PasswordKey`，用户运行 `senv env get FOO` 并输入正确密码
- **THEN** 系统报告 metadata 与数据不同步（而非 `invalid password`），且不泄露明文内容

#### Scenario: 仅密码错误时仍报密码错

- **WHEN** metadata 与数据文件一致，用户运行 `senv env get FOO` 并输入错误密码
- **THEN** 系统照常报告 `invalid password`

#### Scenario: session start 失败时区分原因

- **WHEN** 用户运行 `senv session start`，cache 与 metadata 不同步，输入正确密码
- **THEN** 系统报告数据不同步诊断，而非笼统的密码错误

### Requirement: stale session 处理为非破坏性

`GetCachedKey` 在检测到 stale 会话时 MUST NOT 调用 `clearCache()` 删除缓存。session cache 的清除 SHALL 仅发生在：用户显式执行 `senv session clear`，或 `senv session start` 成功后覆写缓存。系统 SHALL 提供无需校验、不清缓存的诊断访问（如 `PeekCachedKey`），以便在 stale 时仍可探查旧 key 是否能恢复数据。

#### Scenario: stale 时不清缓存保留恢复钥匙

- **WHEN** 存在一个 stale session cache（key 能解开数据但 salt 与 metadata 不符），用户运行任意命令
- **THEN** 系统不删除该 cache，`session status` 仍能读到它，旧 key 可被诊断探针使用

#### Scenario: password 校验成功后清理已知无用 stale cache

- **WHEN** stale session 触发密码回退，且 `VerifyPassword` 成功（证明 metadata 与密码一致，cache 已过期）
- **THEN** 系统 SHALL 清除该 stale cache，使后续命令不再命中它

#### Scenario: 仅 session clear 与 session start 清缓存

- **WHEN** 用户执行 `senv session clear`，或 `senv session start` 成功
- **THEN** 旧 cache 被清除/覆写；其它路径均不主动清缓存

### Requirement: 单次进程内鉴权复用

在同一次 CLI 进程中，系统 SHALL 在首次成功完成鉴权（命中有效 session cache，或交互式密码校验成功）后缓存该认证结果；同一进程内后续任何需要解密的入口（含 `getEnvManager`、`getTextManager`、`getConfigManager`、`resolveValue`/`newCombinedGetter`、TUI/interactive 等）MUST 复用该结果，MUST NOT 再次提示密码。鉴权失败 MUST NOT 写入该缓存。该进程内缓存 MUST NOT 写入 session cache 文件（落盘仍仅由 `senv session start` 负责）。

#### Scenario: env export 无 session 只问一次密码

- **WHEN** 无有效 session，用户在交互式终端运行 `senv env export` 并输入一次正确密码
- **THEN** 命令成功输出 export 语句，过程中密码提示至多一次（含引用解析），且 `senv session status` 仍无 active session

#### Scenario: 同进程 resolveValue 不再二次鉴权

- **WHEN** 无有效 session，某命令已通过密码完成一次 `resolveAuth`，随后在同进程内调用 `resolveValue`（其内部需 env 与 text manager）
- **THEN** 系统不再提示密码并完成解析

#### Scenario: 鉴权失败不污染复用缓存

- **WHEN** 无有效 session，用户输入错误密码
- **THEN** 系统报告 `invalid password`，且同进程内下一次需要鉴权的调用仍可再次提示密码（未缓存失败结果）

### Requirement: 非交互或被捕获的 export 无 session 时禁止弹密码

当不存在有效 session，且满足以下任一条件时，系统 MUST NOT 提示密码，MUST 通过 stderr（或错误返回）清楚提示用户运行 `senv session start`：

1. 标准输入不是交互式终端；或
2. 命令为 `senv env export` 且标准输出不是交互式终端（例如被 `eval $(...)` 捕获）

系统 MAY 提供 `senv env export --if-session`：无有效 session 时 MUST 以空 stdout、成功退出码结束且不提示密码，供 shell 启动脚本使用。

#### Scenario: eval 捕获 export 且无 session

- **WHEN** 无有效 session，用户执行 `eval $(senv env export)`（export 的 stdout 非 TTY）
- **THEN** 系统不提示密码；stderr 含引导执行 `senv session start` 的说明；不写入 session cache

#### Scenario: export --if-session 无 session 静默跳过

- **WHEN** 无有效 session，用户运行 `senv env export --if-session`
- **THEN** stdout 为空、退出码为成功，且不提示密码

#### Scenario: 有 session 时 eval export 仍静默成功

- **WHEN** 存在有效 session，用户执行 `eval $(senv env export)`
- **THEN** 系统不提示密码并输出可 eval 的 export 语句


### Requirement: session 缓存仅驻留 tmpfs

所有 timeout 模式（duration/restart/never）的 session 缓存 SHALL 仅写入 tmpfs（`XDG_RUNTIME_DIR`），MUST NOT 将派生钥写入 `~/.local/share` 等持久化用户数据目录；`never` 仅表示不设时间过期，MUST NOT 再承诺跨重启留存。系统 SHALL 在写缓存时清理历史遗留的持久化缓存文件。

#### Scenario: never session 不再落盘用户数据目录

- **WHEN** 用户执行 `senv session start --timeout never` 后检查 `~/.local/share/senv/session/`
- **THEN** 该目录下不存在缓存文件，缓存仅存在于 `XDG_RUNTIME_DIR` 下且 0600 权限

#### Scenario: never session 在重启后失效

- **WHEN** never 会话建立后系统重启，用户再次运行需要解密的命令
- **THEN** 缓存不存在，系统提示重新解锁（提示输出口令），不产生解密失败误报

#### Scenario: 遗留持久缓存被清理

- **WHEN** 旧版本创建过持久缓存且用户以任意模式重新 start session
- **THEN** `~/.local/share/senv/session/` 下的遗留缓存文件被删除

### Requirement: 缓存文件创建加固

session 缓存写入 SHALL 使用独占创建（`O_EXCL`），并拒绝跟随符号链接；当 `XDG_RUNTIME_DIR` 不可用而回退到 `/tmp` 时，SHALL 使用随机命名的 0700 私有目录；生成会话标识等随机数操作失败时 MUST 报错中止，MUST NOT 以零值或固定值继续。

#### Scenario: 回退路径不可预测

- **WHEN** 环境无 `XDG_RUNTIME_DIR`，用户启动 session
- **THEN** 缓存写入 `/tmp` 下随机命名的 0700 目录内，文件不可被预判路径抢先创建或替换

#### Scenario: 随机数失败即中止

- **WHEN** 会话标识生成所需的随机数读取失败
- **THEN** `senv session start` 返回错误并退出，不落盘任何缓存
