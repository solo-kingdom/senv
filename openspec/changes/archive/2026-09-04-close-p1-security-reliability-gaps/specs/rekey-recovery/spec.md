## Purpose

保证 vault 改口令与密钥轮换在任意文件系统错误、进程崩溃或机器重启后都不会暴露混合密钥状态，并能确定性恢复为完整可解锁的旧版本或新版本。

## ADDED Requirements

### Requirement: rekey 预检完整且失败关闭

系统 SHALL 在修改任何密文或 metadata 前完整枚举并以旧 key 验证所有受管 env、text、config 密文及 config index。任一遍历、读取、索引解析、身份校验或解密错误 MUST 中止 rekey，且不得修改 vault。

#### Scenario: 遍历错误不被忽略
- **WHEN** 枚举受管密文时目录遍历返回权限或 I/O 错误
- **THEN** `senv passwd` 失败，原 metadata 与全部密文保持不变

#### Scenario: config index 损坏时拒绝 rekey
- **WHEN** config index 无法读取、解析或通过身份校验
- **THEN** rekey 在写入前失败，不跳过 config 文件，也不切换 metadata

### Requirement: rekey 保持完整可解锁不变量

rekey SHALL 作为可恢复事务执行。任意单次写入、同步、替换、metadata 切换失败或进程崩溃后，系统 MUST 保证旧 key 对完整旧状态可用，或新 key 对完整新状态可用；MUST NOT 将混合新旧 key 的状态作为可用 vault 暴露。

#### Scenario: 数据切换中途失败
- **WHEN** 多个密文切换过程中任一文件操作失败
- **THEN** 操作返回失败，后续恢复使全部条目与同一版本 metadata 匹配

#### Scenario: metadata 切换失败
- **WHEN** 新密文已准备完成但 metadata 持久化失败
- **THEN** 旧密码仍可解锁完整 vault，且新状态不会被部分启用

#### Scenario: 任意阶段进程崩溃
- **WHEN** rekey 在准备、提交或清理阶段被强制终止并重新启动 senv
- **THEN** 系统先完成恢复，再允许解锁或写入，恢复后至少一个密码可解锁全部条目

### Requirement: 未完成 rekey 自动恢复

任何可能观察或修改 vault 的入口 SHALL 先检测未完成的 rekey。系统 SHALL 根据已持久化的事务状态确定性完成提交或回滚；若不能安全恢复，MUST fail closed、保留恢复材料并给出不要求泄露秘密的诊断。

#### Scenario: 启动时发现可回滚事务
- **WHEN** 启动时发现提交点之前中断的 rekey
- **THEN** 系统恢复完整旧状态、清理已确认无用的新临时文件，然后继续正常解锁

#### Scenario: 恢复本身失败
- **WHEN** 自动恢复遇到磁盘只读或 I/O 错误
- **THEN** 系统拒绝普通读写与再次 rekey，保留事务材料并报告恢复失败

### Requirement: rekey 完成后安全清理

系统 MUST 仅在新 metadata 与全部新密文已持久化且恢复状态标记为完成后，才删除旧版本和事务记录。rekey 期间同一 vault 的其他写操作 MUST 被拒绝或串行等待。

#### Scenario: 成功完成 rekey
- **WHEN** 全部新密文和 metadata 已可靠提交
- **THEN** 新密码可解锁全部条目，旧密码不可用，旧版本与事务记录被安全清理

#### Scenario: 并发写入
- **WHEN** rekey 进行中另一个进程尝试修改同一 vault
- **THEN** 该写入不会与 rekey 交错，也不会产生未被事务覆盖的条目
