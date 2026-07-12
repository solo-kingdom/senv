## ADDED Requirements

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
