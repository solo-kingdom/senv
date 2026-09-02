## ADDED Requirements

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
