## Purpose

提供 SSH host 与 keypair 的一等加密管理：结构化字段与自由扩展、host→keypair 引用一致性、ssh config 导出与私钥落地，并集成 TUI 浏览与 MCP 只读查询。

## ADDED Requirements

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
删除被 host 引用的 keypair 时默认 SHALL 拒绝并列出引用者；`--force` SHALL 删除并同时清空这些 host 的 `identityKey`。

#### Scenario: Delete referenced keypair
- **WHEN** keypair `web-key` 被 host `web` 引用，执行 `senv keypair delete web-key`
- **THEN** 系统 SHALL 拒绝并列出引用者 `web`

#### Scenario: Force delete clears references
- **WHEN** 执行 `senv keypair delete web-key --force`
- **THEN** keypair SHALL 被删除，且 host `web` 的 `identityKey` SHALL 被清空

### Requirement: 导出 OpenSSH config 片段
`senv host export` SHALL 生成 OpenSSH config 片段：默认全部 host，`--host <alias>` 可过滤；核心字段映射为 `Host`/`HostName`/`User`/`Port`/`ProxyJump`，额外 KV 直传为 OpenSSH 关键字，`IdentityFile` 统一指向 `~/.ssh/senv/<keypair name>`。`proxyJump` 引用缺失时 MUST 报错。

#### Scenario: Export all hosts
- **WHEN** 执行 `senv host export`
- **THEN** 输出 SHALL 为每个 host 生成 `Host <alias>` 块，含 HostName/User/Port/ProxyJump/IdentityFile 与额外 KV

#### Scenario: Export single host
- **WHEN** 执行 `senv host export --host web`
- **THEN** 输出 SHALL 仅包含 host `web` 的块

#### Scenario: Export with dangling proxyJump
- **WHEN** 某 host 的 `proxyJump` 指向已不存在的 alias
- **THEN** `export` SHALL 报错并指出该 host

### Requirement: keypair materialize 落盘
`senv keypair materialize <name>` SHALL 将私钥解密写入 `~/.ssh/senv/<name>`（目录 0700、文件 0600）；目标已存在时默认 SHALL 拒绝覆盖。

#### Scenario: Materialize to convention directory
- **WHEN** 执行 `senv keypair materialize web-key`
- **THEN** `~/.ssh/senv/web-key` SHALL 存在且权限为 0600，目录权限为 0700

#### Scenario: Refuse overwrite
- **WHEN** 目标文件已存在且未指定 `--force`
- **THEN** 系统 SHALL 报错拒绝覆盖

### Requirement: TUI 浏览与 MCP 只读集成
TUI SHALL 提供 host/keypair 浏览板块，私钥内容 SHALL 遮蔽（展示指纹/公钥摘要）。MCP SHALL 提供只读的 host 列表/详情工具，MUST NOT 提供返回私钥明文的工具。

#### Scenario: TUI masks private key
- **WHEN** 在 TUI 中查看 keypair `web-key`
- **THEN** 界面 SHALL 显示指纹与元信息，私钥内容 SHALL 遮蔽

#### Scenario: MCP read-only host query
- **WHEN** agent 通过 MCP 查询 host 列表
- **THEN** 返回 SHALL 包含连接信息与指纹，MUST NOT 包含私钥明文

### Requirement: 加密与同步不变性
host/keypair 数据 SHALL 以密文进入 vault，并复用现有 git/server 同步通道（零知识，仅见密文）。

#### Scenario: Sync contains ciphertext only
- **WHEN** 执行 push/pull（git 或 server 模式）
- **THEN** 同步载体中 SHALL 只包含加密后的 host/keypair 数据
