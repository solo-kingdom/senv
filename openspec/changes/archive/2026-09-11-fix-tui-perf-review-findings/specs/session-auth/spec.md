## MODIFIED Requirements

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
