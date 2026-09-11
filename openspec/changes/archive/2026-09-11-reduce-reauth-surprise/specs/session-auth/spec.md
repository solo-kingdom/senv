## MODIFIED Requirements

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
