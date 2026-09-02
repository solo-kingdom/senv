# server-sync Specification

## Purpose
定义 CLI 在 server provider 模式下的端到端行为：初始化与解锁、本地缓存读写、增量同步、乐观锁冲突处理与离线兜底。
## Requirements
### Requirement: server 模式初始化与解锁

系统 SHALL 支持以 server 地址 + token 初始化 vault：拉取 server 托管的 metadata blob 落盘为本地缓存，之后用 vault 口令解锁，解锁机制与本地模式一致（PBKDF2 派生 + passwordKey 校验）。vault 口令 MUST NOT 发送到 server。

#### Scenario: 新机器接入已有 vault

- **WHEN** 用户在新机器以 server 地址 + token 初始化已存在的 vault，并输入正确 vault 口令
- **THEN** 拉取 metadata 与全部条目建立本地缓存，解锁成功，可正常读写

#### Scenario: 口令错误

- **WHEN** 初始化时 vault 口令错误
- **THEN** 解锁失败并提示口令错误，本地缓存已落盘但不泄露任何明文

### Requirement: 本地缓存为工作副本

server 模式下所有读写操作 SHALL 直接作用于本地缓存，格式与现有本地存储一致。读写 MUST NOT 因 server 不可达而失败。

#### Scenario: 离线读写

- **WHEN** server 不可达时执行 env set / text 写入 / config 编辑
- **THEN** 操作成功写入本地缓存，并标记有待推送更改

### Requirement: 增量同步

同步 SHALL 先 pull（按 `last_synced_revision` 增量拉取并落盘）再 push（批量提交本地待推送更改）。两端一致时 SHALL 提示已是最新并成功退出。同步成功后 MUST 更新 `last_synced_revision`。

#### Scenario: 正常双向同步

- **WHEN** 本地有新更改且远端有新更改（无冲突）
- **THEN** 远端更改落盘、本地更改推送成功，报告同步结果

### Requirement: 冲突检测与人工解决

push 返回冲突时，MUST 向用户列出冲突条目标识，MUST NOT 自动覆盖任一端数据，MUST 给出解决指引（如拉取后重新编辑或强制覆盖的命令）。v1 SHALL 只检测不自动合并。

#### Scenario: 写冲突

- **WHEN** 本地修改的条目在远端已被更新，执行同步
- **THEN** 同步中止并列出冲突条目，两端数据均保持不变

### Requirement: 离线兜底与恢复

server 不可达时同步命令 SHALL 明确报告网络失败且本地数据不受影响；可达性恢复后，下一次同步 MUST 能正常完成全部积压更改的推送与拉取。

#### Scenario: 断网后恢复

- **WHEN** 断网期间本地产生多次更改，恢复网络后执行同步
- **THEN** 全部积压更改推送成功，远端新更改落盘，状态收敛一致

