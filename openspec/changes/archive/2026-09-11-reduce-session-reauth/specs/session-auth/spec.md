## MODIFIED Requirements

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

## ADDED Requirements

### Requirement: 失效判定分三层且不可判定不改状态

系统 SHALL 把持久会话不可复用归因为三类之一：**到期**（`expires_at` 已过）、**失效**（成立前提不再满足，如 `restart` 会话遇 boot ID 变化或 vault 身份变化）、**不可判定**（环境故障，如 boot ID 不可读、平台安全存储暂不可用、缓存内容损坏、同一槽位出现多份缓存）。只有到期与失效 MAY 清缓存并回退重新认证；不可判定 MUST 保留缓存、MUST NOT 删除、MUST NOT 降级为跳过校验复用未经验证的 key，并 MUST 返回说明原因与下一步的可操作错误。

#### Scenario: 不可判定不删除缓存

- **WHEN** boot ID 不可读（如容器内无 `/proc`），用户运行需要解密的命令
- **THEN** 命令报告不可判定并说明原因，缓存文件仍存在；环境恢复后同一缓存免口令复用，不要求重新认证

#### Scenario: 多缓存不按到期处理

- **WHEN** 平台安全存储与磁盘逃生舱在同一 vault 槽位各有一份可读缓存
- **THEN** 命令报告可操作错误并提示 `senv session clear --all`，MUST NOT 静默删除缓存或按过期回退口令

#### Scenario: 到期才清缓存

- **WHEN** `duration` 会话的 `expires_at` 已过
- **THEN** 缓存被清理并回退重新认证，且报告原因为到期

#### Scenario: 失效按原因报告

- **WHEN** `restart` 会话的 boot ID 与当前系统不一致
- **THEN** 系统报告会话因系统重启失效，而非笼统的「已过期」

### Requirement: 持久会话生命周期按类型判定

`duration` 会话 SHALL 只按 `expires_at` 判到期，系统重启 MUST NOT 使其失效。`restart` 会话 SHALL 以建立时记录的 boot ID 判失效。两种类型的缓存都 MUST 通过 metadata salt 匹配与 key 验证后才可复用。

#### Scenario: 重启后 duration 会话仍有效

- **WHEN** 用户建立 8h `duration` 会话后重启系统，并在 8h 内再次运行需要解密的命令
- **THEN** 系统复用该会话、不提示密码

#### Scenario: restart 会话重启后失效

- **WHEN** `restart` 会话建立后系统重启，用户再次运行需要解密的命令
- **THEN** 缓存判为失效并提示重新认证，不产生解密失败误报

### Requirement: 会话缓存按 vault 分槽

缓存 SHALL 按 vault 分槽：vault 身份以规范化后的 data path（绝对化、`Clean`、解析符号链接）哈希表示；同一 vault 的等价写法 MUST 共用同一槽位。不同 vault MUST 互不覆写。`senv session clear` 默认 SHALL 只清当前 vault 的槽位，`--all` SHALL 清全部槽位与旧单槽残留。系统 SHALL 在首次读取时按 data path hash 收养旧单槽缓存；hash 不匹配的旧缓存 MUST 原样保留并提示一次 `senv session clear --all`，MUST NOT 因旧文件存在而按多缓存报错或强制重新认证。

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

### Requirement: 持久会话续期

`duration` 会话 SHALL 在业务命令实际复用缓存时续期：`expires_at` 更新为 `min(now + timeout, created_at + max_lifetime)`；`max_lifetime` 默认 24h 且 SHALL 可配置，显式 `--timeout` MUST NOT 被默认上限削减。只有实际复用缓存的业务命令触发续期，`session status` / `doctor` 等只读命令 MUST NOT 续期。系统 SHALL 提供 `senv session refresh`：对当前 vault 已验证有效的会话续期，MUST NOT 提示密码；会话到期、失效或不可判定时 MUST 报告原因与下一步，MUST NOT 静默新建会话。会话仍有效时 `senv session start` MUST 直接续期、MUST NOT 提示密码；只有显式口令重新认证才重置 `created_at`。跨进程自动重建持久会话 SHALL 默认关闭，仅可由用户显式开启。

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

#### Scenario: 自动重建默认关闭

- **WHEN** 无有效 session，用户在交互式终端运行 `senv env get FOO` 并输入正确密码（未开启自动重建）
- **THEN** 命令成功，但 `senv session status` 仍显示无 active session

### Requirement: 失效原因可见与审计

`senv session status` SHALL 区分：无会话、Active（含剩余时间）、到期、失效（含原因）、不可判定（含原因），且 SHALL 说明缓存是否保留。系统 SHALL 把到期、失效、不可判定记录为本地审计事件（`session_expire` / `session_invalidated` / `session_unverifiable`），字段 MUST NOT 含 key、salt、口令或任何明文。

#### Scenario: status 显示不可判定与缓存保留

- **WHEN** 当前会话不可判定（如 boot ID 不可读）
- **THEN** `senv session status` 输出不可判定状态、原因与「缓存已保留」，而不是「Expired」

#### Scenario: 审计记录到期事件

- **WHEN** `duration` 会话到期并触发重新认证
- **THEN** 本地审计新增 `session_expire` 事件，含会话 ID、timeout 类型与原因

#### Scenario: 审计不含密钥材料

- **WHEN** 查看任一新增会话事件的审计记录
- **THEN** 记录中不含 key、salt、口令或明文内容
