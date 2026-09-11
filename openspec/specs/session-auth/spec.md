# session-auth Specification

## Purpose

统一 senv 各命令入口的 session 认证契约：有有效 session 则复用 derived key；无 session 时功能内密码仅作临时认证；仅 `senv session start` 写入 session cache。
## Requirements

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

### Requirement: 功能内密码仅临时认证

当不存在有效 session 时，系统 MAY 在**交互式**功能命令中提示密码以完成当次操作；该密码认证 MUST NOT 创建或刷新 session cache（唯一例外是用户显式开启的自动重建，见「持久会话续期」）。写入或更新 session cache SHALL 仅限 `senv session start`、`senv session refresh`，以及有效 `duration` 会话在业务命令复用时的续期。同一进程内首次密码成功后，后续入口 MUST 按「单次进程内鉴权复用」复用结果，MUST NOT 再次提示。非交互或被捕获的 `env export` 路径 MUST 遵守「禁止弹密码并提示 session start」的要求，不得以多次临时密码代替 session。

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

在同一次 CLI 进程中，系统 SHALL 在首次成功完成鉴权（命中有效 session cache，或交互式密码校验成功）后缓存该认证结果；同一进程内后续任何需要解密的入口（含 `getEnvManager`、`getTextManager`、`getConfigManager`、`resolveValue`/`newCombinedGetter`、TUI/interactive 等）MUST 复用该结果，MUST NOT 再次提示密码。鉴权失败 MUST NOT 写入该缓存。该进程内缓存 MUST NOT 写入 session cache 文件（落盘仅由 `senv session start`、`senv session refresh` 与有效会话续期负责）。

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

### Requirement: session 缓存仅驻留平台安全存储

所有 timeout 模式（duration/restart）的 session 缓存 SHALL 按同一套 Unix 文件系统选型写入，操作系统钥匙串（含 macOS 登录钥匙串）MUST NOT 作为安全存储或可选后端：

- 安全存储 SHALL 仅为经操作系统确认的 memory-backed 文件系统（tmpfs/ramfs）。所有平台 MUST 校验候选路径（`XDG_RUNTIME_DIR` 与任何 fallback）的实际 backing filesystem，不得仅依据环境变量名或 `/tmp` 路径推断。
- 当安全存储可用时，session 缓存 MUST 写入安全存储。
- 当安全存储不可用时：Linux MUST fail closed，除非用户显式开启磁盘逃生舱；无法证明 memory-backed 的平台（含 stock Darwin）MUST 将磁盘逃生舱作为默认写目标，并在 `session start` 写入时输出醒目安全警告。
- 系统 MUST NOT 调用钥匙串读写或删除会话缓存；遗留钥匙串 item MUST NOT 被读取或收养。
- 读路径为同一 vault slot 同时读到安全存储缓存与磁盘逃生舱缓存时，SHALL 确定性地选用其一而非硬失败；无论选中哪个，其 MUST 通过完整有效性校验，未被选中的缓存 MUST NOT 被删除（它可能是另一 slot 的唯一恢复钥匙或便于排查）。

磁盘逃生舱（显式或因无法提供安全存储而默认）MUST 保持 0600/0700 权限、独占/原子写入与 boot ID 校验。错误信息在 fail closed 时 MUST 给出可行动指引（平台推荐存储与逃生舱说明）。读路径最终选中磁盘逃生舱缓存时——无论它以较新胜过同时可读的安全存储缓存，还是安全存储读取失败后的回退——系统 SHALL 向 stderr 输出安全警告（同进程至多一次，语义与写路径警告一致：派生会话密钥以明文存储在 0600 磁盘文件中），且 SHALL 提供进程内状态供支持操作审计的命令在审计中留下「缓存来自磁盘逃生舱」的记录；MUST NOT 在无任何警告或审计痕迹的情况下静默使用磁盘逃生舱缓存完成解密。

系统 SHALL 在写缓存时清理历史遗留的 `~/.local/share/senv/session/` 持久化缓存文件。安全存储不承诺跨重启留存。磁盘逃生舱上 duration 会话按 `expires_at` 跨重启仍有效；`restart` 与重启失效语义在所有平台保持不变。

文件系统候选路径 SHALL 先经可信解析再校验组件：系统自带的符号链接（如 macOS `/var` → `/private/var`）MUST NOT 导致误拒；解析后的路径仍 MUST 无符号链接组件，写入 MUST 继续拒绝跟随目标及父路径中的符号链接。

#### Scenario: macOS 默认使用 Keychain

- **WHEN** 在 Darwin 上执行 `senv session start`（无论 login keychain 是否可用）
- **THEN** 系统不写入钥匙串；无法证明 memory-backed 时写入磁盘逃生舱并警告，能证明 tmpfs/ramfs 时写入安全存储

#### Scenario: stock Darwin 默认磁盘逃生舱

- **WHEN** 在无法证明 memory-backed 的 Darwin 上执行 `senv session start` 且未传 `--insecure-cache`
- **THEN** 命令成功，stderr 含醒目安全警告，session cache 以 0600 写入磁盘逃生舱；后续命令与 MCP 读取不触发 GUI 授权

#### Scenario: Darwin 证明 tmpfs 时走安全存储

- **WHEN** Darwin 上 `XDG_RUNTIME_DIR`（或合格 fallback）经校验为 tmpfs/ramfs，用户执行 `senv session start` 且未开磁盘逃生舱
- **THEN** session cache 写入该内存文件系统，不写入磁盘逃生舱

#### Scenario: 不调用钥匙串且不收养遗留 item

- **WHEN** 登录钥匙串中仍有旧版 senv session item，用户执行 `session start` / 任意读会话 / `session clear` / `session clear --all`
- **THEN** 系统不调用钥匙串；不读取、不删除该 item；新会话只写入 Unix 文件系统选型的目标

#### Scenario: macOS Keychain 不可用 fail closed

- **WHEN** Darwin 上 Keychain 锁定或不可用（如未解锁的 headless SSH 会话），且用户未显式开启 `--insecure-cache`
- **THEN** `session start` 不因钥匙串失败而退出；按 Unix 选型写入安全存储或默认磁盘逃生舱，不调用钥匙串

#### Scenario: restart session 不再落盘用户数据目录

- **WHEN** 用户执行 `senv session start --timeout restart` 且平台安全存储可用
- **THEN** `~/.local/share/senv/session/` 下不存在缓存文件，缓存仅位于平台安全存储且文件权限满足存储介质的私有约束

#### Scenario: restart session 在重启后失效

- **WHEN** restart 会话建立后系统重启，用户再次运行需要解密的命令
- **THEN** 缓存的 boot ID 校验失败，系统提示重新解锁，不产生解密失败误报

#### Scenario: Darwin 磁盘逃生舱上 duration 跨重启仍有效

- **WHEN** stock Darwin 上存在未到期的 duration 会话缓存（磁盘逃生舱），系统重启后 boot ID 已变
- **THEN** 在 `expires_at` 之前命令仍复用该会话，不因 boot ID 变化要求重新认证

#### Scenario: 遗留持久缓存被清理

- **WHEN** 旧版本创建过持久缓存且用户以任意模式重新 start session
- **THEN** `~/.local/share/senv/session/` 下的遗留缓存文件被删除

#### Scenario: disk-backed XDG runtime 被拒绝

- **WHEN** Linux 上 `XDG_RUNTIME_DIR` 指向磁盘文件系统且未显式开启磁盘逃生舱
- **THEN** `session start` fail closed、说明介质不安全，且不写入派生钥

#### Scenario: disk-backed fallback 被拒绝

- **WHEN** Linux 上 `XDG_RUNTIME_DIR` 为空且系统临时目录不是 memory-backed，且未显式开启磁盘逃生舱
- **THEN** 所有 timeout 模式均拒绝创建 cache，不回退到持久盘

#### Scenario: 系统符号链接不再误伤

- **WHEN** 候选 runtime 路径包含系统自带的符号链接（如 `/var/folders/...`）
- **THEN** 系统解析为真实路径后继续校验；若解析后介质合格则正常使用，若仍含符号链接或介质不合格则拒绝

#### Scenario: 显式 opt-in 磁盘逃生舱

- **WHEN** 用户显式开启磁盘逃生舱并执行 `session start`
- **THEN** 系统输出醒目安全警告，cache 以 0600 文件、0700 目录、原子写入与 boot ID 校验写入磁盘

#### Scenario: 逃生舱默认关闭

- **WHEN** Linux 上平台安全存储不可用且用户未显式开启磁盘逃生舱
- **THEN** 所有 timeout 模式均拒绝创建 cache，不写入任何磁盘文件

#### Scenario: 双缓存时逃生舱胜出输出警告

- **WHEN** 同一 slot 同时读到可读的安全存储缓存与磁盘逃生舱缓存，逃生舱缓存较新被选中
- **THEN** 进程继续以逃生舱缓存完成解密，但 stderr 输出磁盘逃生舱安全警告（同进程至多一次）；安全存储缓存不被删除

#### Scenario: 安全存储读取失败回退逃生舱输出警告

- **WHEN** 安全存储读取失败（锁定/不可用），同 slot 存在可用的磁盘逃生舱缓存并被选中
- **THEN** 命令成功完成，stderr 输出磁盘逃生舱安全警告（同进程至多一次）

#### Scenario: 逃生舱选中留审计痕迹

- **WHEN** 支持操作审计的命令使用磁盘逃生舱缓存完成解密
- **THEN** 操作审计可查到本次执行选中了磁盘逃生舱缓存，记录不含密钥明文

#### Scenario: 仅安全存储时不产生逃生舱警告

- **WHEN** 某 slot 只存在安全存储缓存且读取成功
- **THEN** 不输出磁盘逃生舱安全警告，审计不留逃生舱记录

### Requirement: 超时值校验

session 超时值 SHALL 仅接受 duration（`30m`/`8h`/`1d`/`1y` 等）与 `restart`。`never`、`infinite`、`forever` SHALL 被拒绝，错误信息 MUST 列出支持的取值。settings 中 `session.timeout` 为 `never` 时，`senv session start`（未显式传 `--timeout`）MUST 报错并给出可用值与修正指引。历史遗留 cache 的 `timeout_type` 为 `never` 时，系统 SHALL 按 `restart` 语义收养（两者本就等价），MUST NOT 因此强制重新认证，MUST NOT 崩溃或误报解密失败。

#### Scenario: 拒绝 never 超时值

- **WHEN** 用户执行 `senv session start --timeout never`（或 `infinite`/`forever`）
- **THEN** 命令报错，不写入 cache，错误信息列出支持的取值（duration 与 `restart`）

#### Scenario: settings 遗留 never 报错

- **WHEN** settings.json 配置 `"timeout": "never"` 且用户执行不带 `--timeout` 的 `senv session start`
- **THEN** 命令报错并提示更新 settings 为受支持的取值

#### Scenario: 遗留 never cache 判为过期

- **WHEN** 旧版本创建的 `timeout_type: "never"` cache 仍存在且 boot ID 未变化，用户运行 `senv session status` 或需要解密的命令
- **THEN** 系统不再把它判为过期，而是按等价的 `restart` 语义收养并直接复用；不要求重新认证，不误报为密码错误或数据损坏

### Requirement: 缓存文件创建加固

session 缓存写入 SHALL 使用独占创建，并拒绝跟随目标及父路径中的符号链接。`XDG_RUNTIME_DIR` 不可用时，系统 MAY 仅在经确认 memory-backed 的临时文件系统中使用随机命名的 0700 私有目录；否则 MUST fail closed。生成会话标识或私有路径所需的随机数失败时 MUST 报错中止，不得以零值或固定值继续。

#### Scenario: 回退路径不可预测

- **WHEN** 环境无 `XDG_RUNTIME_DIR`，但系统临时目录经确认是 memory-backed
- **THEN** 缓存写入随机命名的 0700 私有目录，文件不可被预判路径抢先创建或替换

#### Scenario: fallback 介质不安全

- **WHEN** 环境无 `XDG_RUNTIME_DIR` 且 fallback 无法证明是 memory-backed
- **THEN** session 创建失败，不创建 cache 目录或文件

#### Scenario: 随机数失败即中止

- **WHEN** 会话标识或私有路径生成所需的随机数读取失败
- **THEN** `senv session start` 返回错误并退出，不落盘任何缓存

#### Scenario: cache 路径包含符号链接

- **WHEN** cache 目标或父路径被替换为符号链接
- **THEN** session 创建失败，链接目标保持不变

### Requirement: MCP 请求级 session 授权与撤销

已启动的 MCP server SHALL 在每个可能读取或修改 vault 的工具请求执行前，重新验证启动时授权对应的 data path、metadata salt 与 cached key（含 key 指纹）。授权身份 MUST NOT 绑定 session ID、到期时间或 boot ID：同一 vault、同一派生密钥下，用户重新执行 `senv session start` / `senv session refresh` MUST NOT 使运行中的 MCP server 失效。验证失败 MUST 在调用业务 manager 前拒绝请求、清除进程内 key，并返回要求重新启动 session/MCP server 的错误。

#### Scenario: duration session 到期

- **WHEN** MCP server 启动后原 session 超过 expiry，再调用任一 senv 工具
- **THEN** 请求在读取秘密或写入数据前失败，进程内 key 被清除

#### Scenario: session clear 主动撤销

- **WHEN** 用户在 MCP server 运行期间执行 `senv session clear`（当前 vault）
- **THEN** 下一次工具请求失败，不返回旧 key 可解密的值，也不修改 vault

#### Scenario: session 被替换

- **WHEN** MCP server 运行期间用户对同一 vault 重新执行 `session start` 或 `session refresh`，生成新的 session ID，但 salt 与派生 key 未变
- **THEN** 后续工具请求继续成功，用户不需要重启 agent 的 MCP process

#### Scenario: restart session 保持有效

- **WHEN** memory-backed 的 restart session 未被 clear，且 data path、salt 与 cached key 仍匹配
- **THEN** MCP 请求继续成功，不因没有 expiry 而被误撤销

#### Scenario: metadata salt 改变

- **WHEN** rekey 或同步使 metadata salt 与 MCP 启动时授权不一致
- **THEN** 下一次请求被拒绝，旧 key 不再用于任何 manager 操作

### Requirement: session start 与 vault generation 线性化

`session start` SHALL 在同一 vault generation 内完成口令验证、metadata 读取、key 派生、key 与 metadata 的匹配确认以及 cache 提交。rekey 与 session start MUST 串行化；成功返回的 session cache MUST 对应完成时的 metadata salt 和有效 derived key，不得由并发 rekey 产生已知 stale cache。

#### Scenario: rekey 先完成
- **WHEN** rekey 在 session start 获得 vault 访问权前完成
- **THEN** session start 使用新 generation 验证口令；旧口令被拒绝，或新口令成功建立可立即使用的 cache

#### Scenario: session start 先完成
- **WHEN** session start 在 rekey 前获得 vault 访问权并成功提交 cache
- **THEN** cache 与当时 metadata/key 匹配；后续 rekey 按既有撤销语义使该 cache 失效，而不是让 session start 成功返回 stale cache

### Requirement: fallback session cache 并发创建保持单一有效结果

在没有 `XDG_RUNTIME_DIR` 的已验证 memory-backed fallback 中，并发 session start SHALL 串行化 cache 创建、替换、枚举和旧目录清理。每个成功返回的 start 完成后，至少存在一个属于该 vault 的有效 session cache；清理不得删除另一个仍在建立或刚建立的有效 cache。

#### Scenario: 两个 fallback start 并发执行
- **WHEN** 两个进程同时在同一用户、同一 vault 的 fallback runtime 中启动 session
- **THEN** 两个命令按确定顺序完成，最终保留一个可验证的 session cache，且不会出现两个命令均成功但无 cache 的状态

#### Scenario: fallback 清理遇到并发 cache
- **WHEN** 一个 start 正在清理旧 fallback 目录，另一个 start 正在创建或提交 cache
- **THEN** 清理不删除正在建立或最新有效的 cache；无法安全判定时该 start 返回错误而不是删除候选目录

### Requirement: 失效判定分三层且不可判定不改状态

系统 SHALL 把持久会话不可复用归因为三类之一：**到期**（`expires_at` 已过）、**失效**（成立前提不再满足，如 `restart` 会话遇 boot ID 变化或 vault 身份变化）、**不可判定**（环境故障，如 boot ID 不可读、平台安全存储暂不可用、缓存内容损坏）。系统 MUST NOT 因同一槽位出现多份缓存就直接判为不可判定；多份缓存 SHALL 按下一条「多缓存确定选择」处理。只有**到期** MAY 自动清缓存并回退重新认证；**失效** MUST 保留缓存、MUST 报告原因与下一步，MUST NOT 静默删除；不可判定 MUST 保留缓存、MUST NOT 删除、MUST NOT 降级为跳过校验复用未经验证的 key，并 MUST 返回说明原因与下一步的可操作错误。

#### Scenario: 不可判定不删除缓存

- **WHEN** boot ID 不可读（如容器内无 `/proc`），用户运行需要解密的命令
- **THEN** 命令报告不可判定并说明原因，缓存文件仍存在；环境恢复后同一缓存免口令复用，不要求重新认证

#### Scenario: 到期才清缓存

- **WHEN** `duration` 会话的 `expires_at` 已过
- **THEN** 缓存被清理并回退重新认证，且报告原因为到期

#### Scenario: 失效保留缓存不自动清除

- **WHEN** `restart` 会话的 boot ID 与当前系统不一致，或缓存的 data path hash 属于另一个 vault
- **THEN** 系统报告会话失效及具体原因，缓存仍保留，用户可自行选择 `senv session clear` 或 `senv session start`，MUST NOT 由系统自动删除

#### Scenario: 多缓存不按到期处理

- **WHEN** 平台安全存储与磁盘逃生舱在同一 vault 槽位各有一份可读缓存
- **THEN** 系统按下一条「多缓存确定选择」处理：选择 `created_at` 更新的一份完成校验并复用，保留另一份，提示该选择与 `senv session clear --all`；MUST NOT 静默删除缓存或按过期回退口令

#### Scenario: 失效按原因报告

- **WHEN** `restart` 会话的 boot ID 与当前系统不一致
- **THEN** 系统报告会话因系统重启失效，而非笼统的「已过期」

### Requirement: 多缓存确定选择

当同一 vault 槽位同时存在平台安全存储与磁盘逃生舱两份可读缓存时，系统 SHALL 做出确定选择而非一律报错：MUST 选择 `created_at` 更新的一份用于本次校验与复用，MUST 保留另一份不删除，并 MUST 提示哪一份被忽略及如何 `senv session clear --all` 显式清理。被选中的缓存 MUST 通过完整校验（data path 绑定、salt、cached key）后才可复用。若两份 `created_at` 完全相同无法判定，系统 SHALL 报可操作错误并提示 `senv session clear --all`，MUST NOT 静默删除任一缓存。

#### Scenario: 新者优先被复用

- **WHEN** 平台安全存储与磁盘逃生舱在同一槽位各有一份缓存，逃生舱那份 `created_at` 更新
- **THEN** 系统复用逃生舱那份完成校验，保留平台存储那份，并提示该选择与清理方式

#### Scenario: 无法判定时给可操作错误

- **WHEN** 两份可读缓存的 `created_at` 完全相同
- **THEN** 系统报告可操作错误并提示 `senv session clear --all`，MUST NOT 删除任一缓存

### Requirement: 持久会话生命周期按类型判定

`duration` 会话 SHALL 只按 `expires_at` 判到期，系统重启 MUST NOT 使其失效。`restart` 会话 SHALL 以建立时记录的 boot ID 判失效。两种类型的缓存都 MUST 通过 metadata salt 匹配与 key 验证后才可复用。

#### Scenario: 重启后 duration 会话仍有效

- **WHEN** 用户建立 8h `duration` 会话后重启系统，并在 8h 内再次运行需要解密的命令
- **THEN** 系统复用该会话、不提示密码

#### Scenario: restart 会话重启后失效

- **WHEN** `restart` 会话建立后系统重启，用户再次运行需要解密的命令
- **THEN** 缓存判为失效并提示重新认证，不产生解密失败误报

### Requirement: 会话缓存按 vault 分槽

缓存 SHALL 按 vault 分槽：vault 身份以规范化后的 data path（绝对化、`Clean`、解析符号链接）哈希表示；同一 vault 的等价写法 MUST 共用同一槽位。不同 vault MUST 互不覆写。`senv session clear` 默认 SHALL 只清当前 vault 的槽位，`--all` SHALL 清全部槽位与旧单槽残留。系统 SHALL 在首次读取时按 data path hash 收养旧单槽缓存；hash 不匹配的旧缓存 MUST 原样保留并提示一次 `senv session clear --all`，MUST NOT 因旧文件存在而按多缓存报错或强制重新认证。当缓存的 data path hash 与当前 vault 不匹配时，系统 MUST 报告「该缓存属于另一个 vault」并保留该缓存，MUST NOT 自动删除，因为该缓存可能是另一 vault 的唯一恢复钥匙。

#### Scenario: 等价路径写法共用同一会话

- **WHEN** 用户以 `--path` 的尾斜杠、`~/` 相对写法或经符号链接的等价路径运行命令
- **THEN** 系统解析为同一 vault 身份并复用同一会话，不要求重新认证

#### Scenario: 多 vault 各自保留会话

- **WHEN** 用户先后对两个不同 data path 执行 `senv session start`
- **THEN** 两个 vault 各自保留有效会话，第二个 start 不覆写第一个

#### Scenario: clear 默认只清当前 vault

- **WHEN** 用户对当前 vault 执行 `senv session clear`
- **THEN** 只有当前 vault 的槽位被清除，其它 vault 的会话保留

#### Scenario: clear --all 清全部

- **WHEN** 用户执行 `senv session clear --all`
- **THEN** 所有 vault 的槽位与旧单槽残留都被清除

#### Scenario: 升级时收养旧单槽

- **WHEN** 升级前存在 data path hash 与当前 vault 匹配的旧单槽缓存
- **THEN** 系统收养为当前 vault 的槽位并直接复用，不要求重新认证

#### Scenario: 不匹配的旧槽不被删除

- **WHEN** 旧单槽缓存的 data path hash 与当前 vault 不匹配
- **THEN** 旧缓存原样保留，系统提示一次 `senv session clear --all`，且当前 vault 的命令不被该文件阻塞

#### Scenario: vault 不匹配保留缓存

- **WHEN** 读到的缓存 data path hash 属于另一个 vault
- **THEN** 系统报告该缓存属于别的 vault 并保留它，不自动删除，提示用 `senv session clear --all` 或切换回对应 vault

### Requirement: 持久会话续期

`duration` 会话 SHALL 在业务命令实际复用缓存时续期：`expires_at` 更新为 `min(now + timeout, created_at + max_lifetime)`；`max_lifetime` 默认 24h 且 SHALL 可配置，显式 `--timeout` MUST NOT 被默认上限削减。只有实际复用缓存的业务命令触发续期，`session status` / `doctor` 等只读命令 MUST NOT 续期。系统 SHALL 提供 `senv session refresh`：对当前 vault 已验证有效的会话续期，MUST NOT 提示密码；会话到期、失效或不可判定时 MUST 报告具体原因与**一条**确定的下一步动作，MUST NOT 静默新建会话。续期失败时系统 MUST NOT 删缓存、MUST NOT 退回口令提示。会话仍有效时 `senv session start` MUST 直接续期、MUST NOT 提示密码；只有显式口令重新认证才重置 `created_at`。跨进程自动重建持久会话 SHALL 默认关闭，仅可由用户显式开启。

#### Scenario: refresh 免口令续期

- **WHEN** 当前 vault 存在有效 `duration` 会话，用户运行 `senv session refresh`
- **THEN** 系统延长有效期并输出新的到期时间，全程不提示密码，`session ID` 与缓存的 key 不变

#### Scenario: 只读命令不续期

- **WHEN** 用户只运行 `senv session status` 或 `senv doctor`
- **THEN** `expires_at` 不被推迟

#### Scenario: 有效会话上 start 免口令

- **WHEN** 当前 vault 存在有效会话，用户运行 `senv session start --timeout 7d`
- **THEN** 系统直接用缓存 key 续期到新的 timeout，不提示密码，并按新的 timeout 重算上限

#### Scenario: max_lifetime 上限生效

- **WHEN** 用户建立 1h `duration` 会话后持续使用超过 24h，且未显式重新认证
- **THEN** 会话在 `created_at + max_lifetime` 到期并要求重新认证，续期不得越过该上限

#### Scenario: refresh 遇不可判定不改状态

- **WHEN** 会话不可判定（如平台安全存储暂不可用），用户运行 `senv session refresh`
- **THEN** 命令报告原因与下一步，MUST NOT 新建会话、MUST NOT 删除缓存、MUST NOT 退回口令提示

#### Scenario: refresh 遇失效不推回密码

- **WHEN** 会话已失效（如系统重启后 `restart` 会话），用户运行 `senv session refresh`
- **THEN** 命令报告失效原因并给出确定动作（重新 `senv session start` 或 `senv session clear`），MUST NOT 删除缓存、MUST NOT 在 `refresh` 内提示口令

#### Scenario: 自动重建默认关闭

- **WHEN** 无有效 session，用户在交互式终端运行 `senv env get FOO` 并输入正确密码（未开启自动重建）
- **THEN** 命令成功，但 `senv session status` 仍显示无 active session

### Requirement: 失效原因可见与审计

`senv session status` SHALL 区分：无会话、Active（含剩余时间与距 `max_lifetime` 绝对上限的剩余时间）、到期、失效（含具体原因）、不可判定（含原因），且 SHALL 明确说明缓存是否保留、被忽略的多缓存是哪一份。需要重新认证的命令错误 MUST 给出机器可读的重认证根因与一条确定的下一步动作。系统 SHALL 把到期、失效、不可判定记录为本地审计事件（`session_expire` / `session_invalidated` / `session_unverifiable`），字段 MUST NOT 含 key、salt、口令或任何明文。

#### Scenario: status 显示不可判定与缓存保留

- **WHEN** 当前会话不可判定（如 boot ID 不可读）
- **THEN** `senv session status` 输出不可判定状态、原因与「缓存已保留」，而不是「Expired」

#### Scenario: status 显示多缓存选择

- **WHEN** 同一槽位存在两份可读缓存且系统已选定一份
- **THEN** `senv session status` 说明选用了哪一份、忽略了哪一份，以及如何用 `senv session clear --all` 清理

#### Scenario: status 显示绝对上限剩余

- **WHEN** 当前为 Active 的 `duration` 会话
- **THEN** `senv session status` 同时显示 `expires_at` 剩余时间与距 `created_at + max_lifetime` 的剩余时间

#### Scenario: 错误给出单一确定动作

- **WHEN** 某命令因会话到期需要重新认证
- **THEN** 错误信息包含到期原因与一条确定的下一步（如 `senv session start`），MUST NOT 罗列多个互斥选项让用户自选

#### Scenario: 审计记录到期事件

- **WHEN** `duration` 会话到期并触发重新认证
- **THEN** 本地审计新增 `session_expire` 事件，含会话 ID、timeout 类型与原因

#### Scenario: 审计不含密钥材料

- **WHEN** 查看任一新增会话事件的审计记录
- **THEN** 记录中不含 key、salt、口令或明文内容
