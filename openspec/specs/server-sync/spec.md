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

push 返回冲突时，系统 SHALL 先区分"假冲突"与"真实冲突"：本地快照缺失（条目被视为新增）或快照哈希失配，但本地与远端密文字节一致时，MUST 自动收养远端 revision / 哈希进本地同步状态并继续同步，MUST NOT 报冲突、MUST NOT 改动两端数据。仅当两端密文确实不同（真实冲突）时，系统 MUST 向用户列出冲突条目标识与两侧可用的非机密对比信息（本地 base revision、远端 current revision、删除状态、密文大小、hash、可用更新时间），MUST NOT 自动覆盖任一端数据。非交互环境 MUST 保留现有解决指引；交互环境 MUST 在应用前展示覆盖计划并要求显式确认。新增 server 字段缺失时，CLI SHALL 显示可用信息并保持冲突语义。真实内容冲突 v1 SHALL 只检测不自动合并。

#### Scenario: 快照缺失导致的新增误判被自动收养

- **WHEN** 同步状态丢失某条目快照，但该条目本地文件与远端密文一致，执行 senv sync
- **THEN** 同步自动将该条目远端 revision 写入本地快照，同步成功完成且不报冲突，两端数据字节不变

#### Scenario: metadata 快照哈希为空但两端一致

- **WHEN** 同步状态 metadata 哈希为空，本地与远端 metadata blob 一致，执行 senv sync
- **THEN** 同步自动收养远端 metadata 哈希，同步成功完成且不报 metadata 冲突

#### Scenario: 非交互冲突报告

- **WHEN** 在非 TTY 或指定 `--no-interactive` 时同步遇到冲突
- **THEN** 输出条目标识、本地与远端可用的 revision / size / 状态 / 时间信息，以及既有 `--accept-remote` / `--force-push` 指引，且不修改任何一端数据

#### Scenario: 交互冲突概览

- **WHEN** 在 TTY 中执行 `senv sync` 且未指定冲突策略时遇到冲突
- **THEN** 进入冲突解决器，默认显示脱敏摘要，用户可选择查看、合并或解决条目，确认前不写入本地或远端

#### Scenario: 写冲突

- **WHEN** 本地修改的条目在远端已被更新且两端密文不同，执行同步
- **THEN** 同步中止并列出冲突条目及两侧可用对比信息，两端数据均保持不变

### Requirement: 交互式冲突内容对比

交互式冲突解决器 SHALL 允许用户逐条查看本地与远端内容对比。明文查看 MUST 仅在终端可交互且已获得可用 vault key 后发生；无有效 key 时，系统 MAY 在用户明确请求查看时提示输入 vault 口令。未能解密的一侧 SHALL 保持脱敏摘要，MUST NOT 导致另一侧数据被修改。

#### Scenario: 使用会话密钥查看明文

- **WHEN** 用户在冲突解决器中请求查看内容且存在可用会话密钥
- **THEN** 系统按条目类型显示本地与远端的安全对比，环境变量值默认掩码并仅在显式揭示时显示

#### Scenario: 无法解密远端内容

- **WHEN** 远端 metadata 与本地 key 不兼容且用户未提供可解密远端的凭据
- **THEN** 明细视图显示 key 兼容性警告和可用非机密信息，不显示错误解密结果，也不改变冲突状态

### Requirement: 可选 editor 手动合并

交互式冲突解决器 SHALL 为 `text`、UTF-8 `config`、`env` 与 `config_index` 提供可选 editor 手动合并。合并缓冲区 MUST 标明 LOCAL 与 REMOTE 内容；最终结果 MUST 通过类型校验且不残留冲突标记后才可应用。系统 MUST 在应用前暂存并确认结果，并以冲突时远端 current revision 作为推送 base；若远端在此期间再次变化，MUST 重新检测冲突且不应用已过期的合并结果。

#### Scenario: 手动合并文本条目

- **WHEN** 用户对 `text` 冲突选择 editor 合并并保存合法最终文本
- **THEN** 系统显示合并结果摘要，确认后原子更新本地待推送内容并基于远端 current revision 推送

#### Scenario: 保存未解决标记

- **WHEN** 用户退出 editor 后合并缓冲区仍包含 LOCAL/REMOTE 冲突标记
- **THEN** 系统拒绝应用该结果，说明未解决标记，并保留原始冲突与临时前状态

#### Scenario: 不支持手动合并的类型

- **WHEN** 冲突为二进制 config、删除冲突、`env_meta` 或 vault metadata
- **THEN** editor 合并入口不可用或被拒绝，用户仍可选择本地或远端

### Requirement: 冲突解决安全边界

冲突摘要与日志 MUST NOT 输出明文内容或冲突密文。editor 合并 MUST 使用一次性私有目录与私有文件，并在会话结束后清理。vault metadata MUST NOT 提供原始内容编辑，只能显示安全摘要并整体选择本地或远端。

#### Scenario: editor 清理

- **WHEN** editor 正常退出、失败或合并校验失败
- **THEN** 系统清理本次合并使用的一次性私有目录，不把明文写入持久日志

#### Scenario: metadata 安全摘要

- **WHEN** vault metadata 两端均修改
- **THEN** 界面仅显示安全诊断信息和整体选择入口，不暴露 raw metadata blob 或密钥材料

### Requirement: 同步状态防退化校验

持久化同步状态时，系统 MUST 拒绝写入相对现有状态出现退化的内容：entries 数量减少但不存在对应删除标记，或 metadata 哈希从非空变为空。拒绝时 MUST 返回明确错误并保留现有状态文件不变。

#### Scenario: 拦截快照丢失的写入

- **WHEN** 某次同步试图保存比现有状态少一个条目且无对应 tombstone 的状态
- **THEN** 保存被拒绝并报错，磁盘上的状态文件保持原有完整内容

#### Scenario: 拦截 metadata 哈希清空

- **WHEN** 某次同步试图将非空 metadata 哈希覆盖为空串
- **THEN** 保存被拒绝并报错，磁盘上的状态文件保持原有完整内容

### Requirement: 同步状态 vault 绑定与写入来源

同步状态 SHALL 记录其所属 vault 绑定（server 地址指纹与 vault 名）及最近一次写入来源（代码路径、进程 pid、时间）。加载时若绑定与当前配置不符，MUST 拒绝复用该状态并提示用户执行重建；来源字段 MUST NOT 包含敏感内容。

#### Scenario: 拒绝复用其他 vault 的状态

- **WHEN** 同一本地缓存目录被切换到另一个 vault 后执行同步
- **THEN** 同步因状态文件 vault 绑定不符而中止，并提示执行状态重建，MUST NOT 静默沿用旧快照

#### Scenario: 旧版本状态文件兼容

- **WHEN** 加载缺少绑定字段的旧状态文件且当前 vault 与历史一致
- **THEN** 正常加载并在下一次成功写入时补全绑定与来源字段

### Requirement: 离线兜底与恢复

server 不可达时同步命令 SHALL 明确报告网络失败且本地数据不受影响；可达性恢复后，下一次同步 MUST 能正常完成全部积压更改的推送与拉取。

#### Scenario: 断网后恢复

- **WHEN** 断网期间本地产生多次更改，恢复网络后执行同步
- **THEN** 全部积压更改推送成功，远端新更改落盘，状态收敛一致

### Requirement: 读路径自动拉取

server provider 启用自动同步时，读取类命令（env 导出、list、text 读、session、TUI 打开）SHALL 在返回数据前按节流窗口执行 best-effort 增量拉取：距上次成功拉取超过窗口且 server 可达时，先落盘远端更改再返回；否则直接返回本地缓存。拉取 MUST 有超时预算，失败（超时/不可达）MUST 静默降级为本地缓存，MUST NOT 使命令失败或显著增加延迟。本地有未推送更改的条目 MUST NOT 被远端版本覆盖。

#### Scenario: 节流窗口内不产生网络请求

- **WHEN** 距上次成功拉取不足节流窗口时执行读命令
- **THEN** 命令直接使用本地缓存返回，不发起网络请求

#### Scenario: 窗口过期且 server 可达

- **WHEN** 超过节流窗口后执行读命令且 server 可达
- **THEN** 远端更改先落盘并提示更新条数，命令返回的数据为最新内容

#### Scenario: 拉取超时或不可达

- **WHEN** 拉取超过超时预算或 server 不可达时执行读命令
- **THEN** 命令正常返回本地缓存数据，无报错、无可感知额外延迟

### Requirement: 写路径自动推送

server provider 启用自动同步时，写入类命令 SHALL 先完成本地落盘并立即返回结果，随后在进程退出前 best-effort 推送本地待推送更改。推送失败（网络不可达或乐观锁冲突）MUST NOT 使命令失败，MUST 保留待推送状态由后续命令自动重试，且 SHALL 输出一行警告说明待推送条目数与解决方式（冲突时指引 `senv sync`）。

#### Scenario: 写入后无感推送成功

- **WHEN** server 可达时执行写命令
- **THEN** 本地写入成功、更改自动推送，命令输出正常结果且无错误

#### Scenario: 推送时 server 不可达

- **WHEN** 写命令完成本地落盘但推送时 server 不可达
- **THEN** 命令成功返回，输出待推送警告，本地待推送状态保留

#### Scenario: 推送遇到冲突

- **WHEN** 写命令的推送因远端已更新而返回冲突
- **THEN** 命令成功返回本地写入结果，输出冲突警告并列出冲突条目与 `senv sync` 解决指引，两端数据均不被自动覆盖

### Requirement: 关键写阻塞推送

低频关键写操作（修改 vault 口令、初始化后的首次写入）SHALL 在命令内以阻塞方式推送并在返回前确认结果。推送失败时命令 MUST 明确告警（说明其他设备在同步前无法获得此次更改）并指引手动执行 `senv sync`，本地更改保持已生效。

#### Scenario: 修改口令后推送失败

- **WHEN** 修改 vault 口令时 server 不可达
- **THEN** 口令在本地生效，命令输出明确的推送失败告警与 `senv sync` 指引

### Requirement: 并发同步串行化

同一缓存目录上的并发命令执行时，系统 SHALL 对同步段（拉取、推送及同步状态更新）做进程间互斥，MUST NOT 因并发导致同步状态损坏或重复推送同一更改。

#### Scenario: 两个进程同时执行写命令

- **WHEN** 同一台机器上两个 senv 写命令并发执行且都触发推送
- **THEN** 同步段串行执行，同步状态一致，无条目丢失或状态损坏

### Requirement: 自动同步开关与逃生口

settings SHALL 支持按 vault 配置 `auto_sync`（server provider 下默认开启）。关闭时读写命令 MUST NOT 触发任何网络请求，行为回到纯手动 `senv sync` 模式。读命令 SHALL 支持 `--refresh` 绕过节流窗口强制拉取。

#### Scenario: 关闭自动同步

- **WHEN** `auto_sync` 配置为关闭时执行读写命令
- **THEN** 命令不产生任何网络请求，同步仅通过手动 `senv sync` 完成

#### Scenario: 强制刷新

- **WHEN** 在节流窗口内以 `--refresh` 执行读命令且 server 可达
- **THEN** 命令绕过节流窗口执行拉取并返回最新数据

### Requirement: 同步条目标识遵守严格 schema

server 与客户端 SHALL 仅接受已知 kind，并按 kind 校验 grp/key：`env` 要求 grp/key 均为安全单路径段；`env_meta` 要求 grp 为安全单路径段且 key 为空；`text` 要求 grp/key 均为安全单路径段；`config` 要求 grp 为空且 key 为安全单路径段；`config_index` 要求 grp/key 均为空。未知 kind、缺失字段、额外身份字段或不安全路径段 MUST 被拒绝。

#### Scenario: 五种合法 kind 被接受
- **WHEN** push 或 pull 条目分别满足 `env`、`env_meta`、`text`、`config`、`config_index` 的身份 schema
- **THEN** 条目继续按正常同步流程处理

#### Scenario: kind 未知
- **WHEN** 条目的 kind 不在已知白名单中
- **THEN** server 拒绝整批 push，客户端拒绝 apply，且不创建任何本地文件

#### Scenario: kind 字段组合非法
- **WHEN** `config` 携带 grp、`env_meta` 携带 key，或要求的 grp/key 为空
- **THEN** 条目在访问文件系统前被拒绝

#### Scenario: 身份包含路径语义
- **WHEN** grp 或 key 为 `../x`、`a/../../x`、绝对路径、含 `/`、`\\`、NUL、`.` 或 `..`
- **THEN** server 和客户端均返回验证错误，条目不会进入持久化或本地缓存

### Requirement: 客户端不信任远端同步身份

客户端 SHALL 在每次将远端条目映射为路径、写入或删除前独立执行 schema、containment 和无符号链接验证，不得依赖 server 已校验。无效 pull 批次 MUST NOT 更新本地文件、metadata 或同步 revision/state。

#### Scenario: 被攻陷 server 返回穿越条目
- **WHEN** pull 响应包含 `kind=config,key=../escaped`
- **THEN** 客户端终止该批 apply，data/config 根内外均无文件变化，last synced revision 不前进

#### Scenario: 远端删除指向符号链接
- **WHEN** 删除条目的本地目标或父目录是符号链接
- **THEN** 客户端拒绝删除，链接及其目标保持不变，同步状态不提交

### Requirement: server 在事务前验证完整批次

server SHALL 在创建 vault、推进 revision 或写入任一条目前验证 push 批次内全部 entry identity；任一条目无效时 MUST 原子拒绝整批。

#### Scenario: 批次包含一个非法条目
- **WHEN** push 批次同时包含合法条目和一个非法 identity
- **THEN** server 返回可理解的验证错误，不创建 vault、不推进 revision、不写入任何条目


### Requirement: 同步 apply 失败必须报告恢复结果

客户端对已验证 pull 批次的本地 apply SHALL 将被替换、删除或状态更新的缓存条目视为一个可恢复批次。任一前向写入失败时，系统 MUST 尝试恢复每个已变更条目的完整旧内容和同步 state；任一恢复步骤失败时，返回错误 MUST 明确表明 rollback 未完成，且 MUST NOT 将缓存描述为已完整回滚或推进 revision。

#### Scenario: 前向写入失败且恢复成功
- **WHEN** pull apply 在写入后续条目或同步 state 时失败，且所有已变更条目均可恢复
- **THEN** 命令失败，所有已触及缓存条目与同步 state 恢复为操作前内容，revision 不前进

#### Scenario: 恢复写入失败
- **WHEN** pull apply 的前向操作失败，且恢复某个已变更缓存条目时发生 I/O、目录或原子写入错误
- **THEN** 命令返回包含 rollback failure 的可诊断错误，不宣称旧缓存完整，且同步 state 不前进

#### Scenario: 恢复父目录失败
- **WHEN** 恢复被删除条目所需的父目录无法安全创建或访问
- **THEN** 命令返回恢复失败错误，不静默忽略该失败，也不继续提交同步 state
