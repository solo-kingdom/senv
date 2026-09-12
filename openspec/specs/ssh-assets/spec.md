## Purpose

提供 SSH host 与 keypair 的一等加密管理：结构化字段与自由扩展、host→keypair 引用一致性、ssh config 导出与私钥落地，并集成 TUI 浏览与 MCP 只读查询。
## Requirements
### Requirement: KeyPair 导入与存储
系统 SHALL 提供 `senv keypair` 命令组（`import`、`list`、`materialize`、`delete`）。`import` SHALL 从指定私钥文件读取内容并整体加密存储，name 全局唯一。

#### Scenario: Import a private key file
- **WHEN** 用户执行 `senv keypair import web-key --file ~/.ssh/id_ed25519`
- **THEN** 系统 SHALL 读取文件内容并加密存储为 keypair `web-key`，磁盘上不出现私钥明文

#### Scenario: Import with duplicate name
- **WHEN** keypair `web-key` 已存在，用户再次 `senv keypair import web-key --file ...`
- **THEN** 系统 SHALL 报错并拒绝覆盖（除非显式 `--force`）

#### Scenario: Import nonexistent file
- **WHEN** `--file` 指向不存在的路径
- **THEN** 系统 SHALL 报错且不产生任何存储变更

### Requirement: 公钥派生存档
导入成功后系统 SHALL 尽力从私钥派生公钥并存档指纹；无法派生（如 passphrase 加密的私钥）时 SHALL 导入成功、公钥留空并在 `list` 中标注。

#### Scenario: Derive public key on import
- **WHEN** 导入未加密的 OpenSSH/PEM 格式私钥
- **THEN** 系统 SHALL 存档对应公钥，`senv keypair list` SHALL 显示指纹

#### Scenario: Encrypted private key imports without public key
- **WHEN** 导入 passphrase 加密的私钥
- **THEN** 导入 SHALL 成功，公钥留空并在 list 中标注 `pubkey: none`

### Requirement: Host CRUD 与字段模型
系统 SHALL 提供 `senv host` 命令组（`add`、`get`、`edit`、`list`、`delete`）。alias SHALL 全局唯一并作为主键；核心字段为 hostname、user、port、proxyJump、identityKey、tags；除核心字段外 SHALL 接受任意额外 KV 并原样保存。

#### Scenario: Add host with core fields
- **WHEN** 用户执行 `senv host add web --hostname 10.0.0.1 --user deploy --port 2222`
- **THEN** 系统 SHALL 以 alias `web` 存储 host 记录

#### Scenario: Add host with extra attributes
- **WHEN** `senv host add web ... --attr forwardAgent=yes --attr serverAliveInterval=30`
- **THEN** 系统 SHALL 原样保存这两个 KV

#### Scenario: Duplicate alias
- **WHEN** 要添加的 alias 已存在
- **THEN** 系统 SHALL 报错并拒绝

### Requirement: host add 密钥联动
`senv host add` SHALL 支持三种密钥来源：`--keypair <name>` 引用已有 keypair、`--key-file <path>`（配合 `--keypair-name`）一步导入新 keypair 并关联、交互式选择已有 keypair。`identityKey` MUST 指向已存在的 keypair。

#### Scenario: Reference existing keypair
- **WHEN** `senv host add web ... --keypair web-key`，`web-key` 已存在
- **THEN** host 的 `identityKey` SHALL 设为 `web-key`

#### Scenario: One-step import and link
- **WHEN** `senv host add web ... --key-file ~/.ssh/id_ed25519 --keypair-name web-key`
- **THEN** 系统 SHALL 创建 keypair `web-key` 并将 host `web` 的 `identityKey` 关联到它

#### Scenario: Invalid keypair reference
- **WHEN** `--keypair` 指向不存在的 keypair
- **THEN** 系统 SHALL 报错并拒绝创建 host

#### Scenario: Interactive selection
- **WHEN** `senv host add` 未提供密钥参数且 vault 中已有 keypair
- **THEN** 系统 SHALL 列出 keypair 供选择，且可跳过不关联

### Requirement: ProxyJump 引用校验
`proxyJump` 字段 MUST 引用已存在的 host alias，允许多级跳板。

#### Scenario: Valid proxyJump
- **WHEN** `senv host add db ... --proxy-jump web`，host `web` 已存在
- **THEN** 记录 SHALL 保存 `proxyJump: web`

#### Scenario: Invalid proxyJump
- **WHEN** `--proxy-jump` 指向不存在的 alias
- **THEN** 系统 SHALL 报错并拒绝

### Requirement: 删除保护

删除被 host 引用的 keypair 时默认 SHALL 拒绝并列出引用者；`--force` SHALL 删除并同时清空这些 host 的 `identityKey`。TUI SHALL 遵循同一语义：默认拒绝并列出引用者，仅在用户显式确认强制删除后才清引用。

#### Scenario: Delete referenced keypair
- **WHEN** keypair `web-key` 被 host `web` 引用，执行 `senv keypair delete web-key`
- **THEN** 系统 SHALL 拒绝并列出引用者 `web`

#### Scenario: Force delete clears references
- **WHEN** 执行 `senv keypair delete web-key --force`
- **THEN** keypair SHALL 被删除，且 host `web` 的 `identityKey` SHALL 被清空

#### Scenario: TUI 强制删除清引用
- **WHEN** 用户在 TUI 对仍被引用的 keypair 显式确认强制删除
- **THEN** keypair 被删除，引用它的 host `identityKey` 被清空且列表刷新

### Requirement: 导出 OpenSSH config 片段
`senv host export` SHALL 生成 OpenSSH config 片段：默认全部 host，`--host <alias>` 可过滤；核心字段映射为 `Host`/`HostName`/`User`/`Port`/`ProxyJump`，额外 KV 直传为 OpenSSH 关键字，`IdentityFile` 统一指向 `~/.ssh/senv/<keypair name>`。`proxyJump` 引用缺失时 MUST 报错。`IdentityFile` 引用的 keypair 不在本机 vault 时，export SHALL 逐条输出 warning（指明 host 别名与缺失的 keypair 名）并照常生成片段——悬空引用由后续 materialize 或同步补齐收敛，不阻断导出。

#### Scenario: Export all hosts
- **WHEN** 执行 `senv host export`
- **THEN** 输出 SHALL 为每个 host 生成 `Host <alias>` 块，含 HostName/User/Port/ProxyJump/IdentityFile 与额外 KV

#### Scenario: Export single host
- **WHEN** 执行 `senv host export --host web`
- **THEN** 输出 SHALL 仅包含 host `web` 的块

#### Scenario: Export with dangling proxyJump
- **WHEN** 某 host 的 `proxyJump` 指向已不存在的 alias
- **THEN** `export` SHALL 报错并指出该 host

#### Scenario: IdentityFile 引用的 keypair 本机缺失
- **WHEN** 某 host 的 `identityKey` 指向本机 vault 不存在的 keypair（如档案尚未同步到本机）
- **THEN** `export` SHALL 输出逐条 warning 指明该 host 别名与缺失 keypair 名，片段照常生成且该 host 块仍含 `IdentityFile` 路径

#### Scenario: keypair 全部在位时无新增输出
- **WHEN** 所有被引用 keypair 均在本机 vault
- **THEN** `export` 输出 SHALL 与既有行为一致，不产生 warning

### Requirement: keypair materialize 落盘
`senv keypair materialize <name>` SHALL 将私钥解密写入 `~/.ssh/senv/<name>`（目录 0700、文件 0600）；目标已存在时默认 SHALL 拒绝覆盖。

#### Scenario: Materialize to convention directory
- **WHEN** 执行 `senv keypair materialize web-key`
- **THEN** `~/.ssh/senv/web-key` SHALL 存在且权限为 0600，目录权限为 0700

#### Scenario: Refuse overwrite
- **WHEN** 目标文件已存在且未指定 `--force`
- **THEN** 系统 SHALL 报错拒绝覆盖

### Requirement: TUI 编辑与 MCP 只读集成

TUI SHALL 提供 host/keypair 的完整编辑板块：浏览、新建、编辑、删除 host；导入、重命名、删除、materialize keypair；导出 OpenSSH 片段。私钥内容 SHALL 始终遮蔽（仅展示指纹/公钥摘要），明文 MUST NOT 进入 TUI 状态或渲染输出。host 列表 SHALL 显示其关联的 keypair 名称与指纹摘要。编辑 host 时关联 keypair SHALL 通过选择器完成，引用的 keypair 不存在时 MUST 拒绝保存。MCP SHALL 保持只读：提供 host 列表/详情工具，MUST NOT 提供返回私钥明文的工具。

#### Scenario: TUI masks private key
- **WHEN** 在 TUI 中查看 keypair `web-key`
- **THEN** 界面 SHALL 显示指纹与元信息，私钥内容 SHALL 遮蔽

#### Scenario: MCP read-only host query
- **WHEN** agent 通过 MCP 查询 host 列表
- **THEN** 返回 SHALL 包含连接信息与指纹，MUST NOT 包含私钥明文

#### Scenario: TUI 新建 host 并关联 keypair
- **WHEN** 用户在 TUI 新建 host，选择已有 keypair 作为身份密钥并提交
- **THEN** host 保存成功，列表中该 host 显示所关联的 keypair 名称与指纹摘要

#### Scenario: 引用不存在的 keypair 被拒绝
- **WHEN** 用户在 host 表单提交一个指向不存在 keypair 的引用
- **THEN** 表单内联报错，不写入任何数据

#### Scenario: TUI 删除被引用的 keypair
- **WHEN** 用户对仍被 host 引用的 keypair 触发删除
- **THEN** 界面列出引用它的 host 并拒绝删除；用户显式确认强制删除后，这些 host 的 `identityKey` 被清空

#### Scenario: TUI materialize 需确认
- **WHEN** 用户在 TUI 对 keypair 触发 materialize
- **THEN** 显示确认框与目标路径 `~/.ssh/senv/<name>`（0600）；目标已存在时需再次确认；成功后只提示路径，不渲染私钥内容

#### Scenario: TUI 导出 host 片段
- **WHEN** 用户在 TUI 触发导出 OpenSSH 片段
- **THEN** 先展示将写入的内容预览，确认后写目标文件；存在悬空 `proxyJump` 时按既有规则报错

### Requirement: 加密与同步不变性

host/keypair 数据 SHALL 以密文进入 vault，并复用现有 git/server 同步通道（零知识，仅见密文）。server 模式下 host/keypair 以 `ssh_host` / `ssh_keypair` kind 经 syncschema 白名单双向分发，密文落回本机 `hosts/`、`keypairs/` 收集目录；git 模式随 vault 目录整体分发，不经白名单。client 与 server 的白名单来自同一 syncschema 包：新 client 对旧 server push 携带 SSH 条目的批次时，server 事务前整批校验 SHALL 拒绝（发布顺序约束见 ADR-0020）。

#### Scenario: Sync contains ciphertext only

- **WHEN** 执行 push/pull（git 或 server 模式）
- **THEN** 同步载体中 SHALL 只包含加密后的 host/keypair 数据

#### Scenario: server 模式白名单双向分发

- **WHEN** 已升级的 client 与 server 之间执行增量同步
- **THEN** 本机 `hosts/`、`keypairs/` 的档案密文进入待推送集合，远端 SSH 条目 pull 后落回原目录，`senv ssh host list` / `senv keypair list` 可见

### Requirement: KeyPair 重命名

系统 SHALL 提供 `senv keypair rename <old> <new>`，TUI SHALL 提供同名操作。新名称已存在时 MUST 拒绝且不做任何写入；成功时 SHALL 在同一次 mutation 内原子改写所有引用该 keypair 的 host `identityKey`，私钥与公钥内容 MUST 保持不变。命令 SHALL 输出受影响的 host 数量。

#### Scenario: 重命名并联动 host
- **WHEN** keypair `web-key` 被 host `web` 引用，执行 `senv keypair rename web-key prod-key`
- **THEN** keypair 改名为 `prod-key`，host `web` 的 `identityKey` 变为 `prod-key`，输出提示 1 个 host 已更新

#### Scenario: 新名称冲突
- **WHEN** 目标名称已存在
- **THEN** 系统报错拒绝，原 keypair 与所有 host 引用不变

#### Scenario: TUI 内重命名
- **WHEN** 用户在 TUI 对选中 keypair 触发重命名并输入可用新名称
- **THEN** 重命名生效，host 列表立即反映新的关联名称

