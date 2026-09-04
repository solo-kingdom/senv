## MODIFIED Requirements

### Requirement: session 缓存仅驻留 tmpfs

所有 timeout 模式（duration/restart/never）的 session 缓存 SHALL 仅写入经操作系统确认的 memory-backed 文件系统。系统 MUST 验证 `XDG_RUNTIME_DIR` 与任何 fallback 候选路径的实际 backing filesystem，不得仅依据环境变量名或 `/tmp` 路径推断其为 tmpfs。无法确认 memory-backed 时 MUST 拒绝创建 session，且 MUST NOT 将派生钥写入持久盘；`never` 仅表示不设时间过期，不承诺跨重启留存。系统 SHALL 在写缓存时清理历史遗留的持久化缓存文件。

#### Scenario: never session 不再落盘用户数据目录

- **WHEN** 用户执行 `senv session start --timeout never` 且 runtime 路径经确认是 memory-backed
- **THEN** `~/.local/share/senv/session/` 下不存在缓存文件，缓存仅位于已验证介质且权限为 0600

#### Scenario: never session 在重启后失效

- **WHEN** never 会话建立后系统重启，用户再次运行需要解密的命令
- **THEN** 缓存不存在，系统提示重新解锁，不产生解密失败误报

#### Scenario: 遗留持久缓存被清理

- **WHEN** 旧版本创建过持久缓存且用户以任意模式重新 start session
- **THEN** `~/.local/share/senv/session/` 下的遗留缓存文件被删除

#### Scenario: disk-backed XDG runtime 被拒绝

- **WHEN** `XDG_RUNTIME_DIR` 指向磁盘文件系统
- **THEN** `session start` fail closed、说明介质不安全，且不写入派生钥

#### Scenario: disk-backed fallback 被拒绝

- **WHEN** `XDG_RUNTIME_DIR` 为空且系统临时目录不是 memory-backed
- **THEN** 所有 timeout 模式均拒绝创建 cache，不回退到持久盘

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

## ADDED Requirements

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

#### Scenario: never session 保持有效
- **WHEN** memory-backed 的 never session 未 clear、boot ID 未变化且 salt/key 仍匹配
- **THEN** MCP 请求继续成功，不因没有 expiry 而被误撤销

#### Scenario: metadata salt 改变
- **WHEN** rekey 或同步使 metadata salt 与 MCP 启动时授权不一致
- **THEN** 下一次请求被拒绝，旧 key 不再用于任何 manager 操作
