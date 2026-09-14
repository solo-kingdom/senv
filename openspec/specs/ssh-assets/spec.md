## Purpose

提供 SSH host 与 keypair 的一等加密管理：结构化字段与自由扩展、host→keypair 引用一致性、ssh config 导出与私钥落地，并集成 TUI 浏览与 MCP 只读查询。
## Requirements

### Requirement: KeyPair 导入与存储
系统 SHALL 提供 `senv keypair` 命令组（`import`、`list`、`materialize`、`delete`）。`import` SHALL 从指定私钥文件读取内容并整体加密存储，name 全局唯一。`group` 为单值归属字段（空 = 未分组），`import --group` 可设置，`list` 输出在有值时展示。

#### Scenario: Import a private key file
- **WHEN** 用户执行 `senv keypair import web-key --file ~/.ssh/id_ed25519`
- **THEN** 系统 SHALL 读取文件内容并加密存储为 keypair `web-key`，磁盘上不出现私钥明文

#### Scenario: Import with group
- **WHEN** `senv keypair import web-key --file ... --group prod`
- **THEN** 记录 SHALL 保存 `group: prod`，`list` 输出展示该分组

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
系统 SHALL 提供 `senv host` 命令组（`add`、`get`、`edit`、`list`、`delete`）。alias SHALL 全局唯一并作为主键；核心字段为 hostname、user、port、proxyJump、identityKey、group、tags；除核心字段外 SHALL 接受任意额外 KV 并原样保存。`group` 为单值归属字段，空值表示未分组，参与导出组片段归属与 materialize 分组路径推导；group 值禁止包含 `/`（写入与编辑时校验）。`tags` 为多值自由标注，与 group 正交。

#### Scenario: Add host with core fields
- **WHEN** 用户执行 `senv host add web --hostname 10.0.0.1 --user deploy --port 2222`
- **THEN** 系统 SHALL 以 alias `web` 存储 host 记录

#### Scenario: Add host with group
- **WHEN** `senv host add web ... --group prod`
- **THEN** 记录 SHALL 保存 `group: prod`，导出时落入 `groups/prod.conf`

#### Scenario: Add host with extra attributes
- **WHEN** `senv host add web ... --attr forwardAgent=yes --attr serverAliveInterval=30`
- **THEN** 系统 SHALL 原样保存这两个 KV

#### Scenario: Duplicate alias
- **WHEN** 要添加的 alias 已存在
- **THEN** 系统 SHALL 报错并拒绝

#### Scenario: 既有数据无 group 字段
- **WHEN** 读取旧版本写入的 host（无 `group` 字段）
- **THEN** 系统 SHALL 按空 group（未分组）处理，读写均正常，导出落入 `_ungrouped.conf`

#### Scenario: 分组名含路径分隔符
- **WHEN** `senv host add web ... --group a/b` 或编辑为含 `/` 的组名
- **THEN** 系统 SHALL 报错拒绝写入

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
`senv host export` SHALL 以**应用模式**为默认：按分组整树维护 `~/.ssh/senv/`——组片段落于 `groups/<组>.conf`（未分组 `_ungrouped.conf`，目录 0700、文件 0600），整文件从 vault 重渲染、不回读合并；对被引用 keypair SHALL 自动落盘缺失的私钥文件（已存在 MUST 跳过不覆盖；keypair 不在本机 vault 时逐条 warning 且不阻断导出，片段照常生成）；并幂等注册 `~/.ssh/config` 顶部一行 `Include ~/.ssh/senv/groups/*.conf`（文件不存在时创建为 0600，写入前留 `~/.ssh/config.senv-bak`，用户其余内容 MUST NOT 改动）。`--output <path>`（`-` 表示 stdout）SHALL 进入纯渲染模式：写出片段但不写组片段、不落盘、不注册。`--group <名>` 只重建该组片段；`--host <别名>` 重建其所在组整文件；无过滤为全量重建。组内 host 的 ProxyJump 指向组外 host 时，闭包目标 SHALL 并入发起导出的 fragment。组在 vault 中消失（改名/删空）时，全量 export SHALL 删除对应组片段（`--group`/`--host` 单组重建 MUST NOT 触碰其他组文件）。`proxyJump` 引用缺失 MUST 报错。`IdentityFile` SHALL 指向 `~/.ssh/senv/keys/<keypair 分组>/<keypair 名>`。export 对未被任何 host 引用的落盘私钥 SHALL 逐条 warning，MUST NOT 自动删除。真实组名与 `_ungrouped` 冲突时 SHALL 报错拒绝导出。应用导出完成后 SHALL 输出摘要（写入的组片段、落盘的密钥、注册结果、warning 清单）。

#### Scenario: Export all hosts
- **WHEN** 执行 `senv host export`（未给 `--output`）
- **THEN** 系统 SHALL 为每个 host 生成 `Host <alias>` 块（含 HostName/User/Port/ProxyJump/IdentityFile 与额外 KV，写入对应 `groups/<组>.conf`），落盘所有缺失的被引用私钥，保证 `~/.ssh/config` 顶部存在 glob Include 行，并输出摘要

#### Scenario: Export single host
- **WHEN** 执行 `senv host export --host web`，host `web` 属于组 `prod`
- **THEN** 仅 `groups/prod.conf` SHALL 被重建（整文件），其余组片段不动

#### Scenario: Export with dangling proxyJump
- **WHEN** 某 host 的 `proxyJump` 指向已不存在的 alias
- **THEN** `export` SHALL 报错并指出该 host，且不产生任何文件副作用

#### Scenario: keypair 全部在位时无新增输出
- **WHEN** 所有被引用 keypair 均在本机 vault 且落盘文件已存在
- **THEN** `export` SHALL 不产生 warning，摘要中这些密钥均标注 skipped，组片段内容与 vault 一致地重写

#### Scenario: 重复导出幂等
- **WHEN** 再次执行 `senv host export`，组片段与 Include 行已存在、私钥已落盘
- **THEN** Include 行 MUST NOT 重复出现，已落盘私钥 MUST 跳过，组片段内容与 vault 一致地重写

#### Scenario: 纯渲染模式无副作用
- **WHEN** 执行 `senv host export --output -` 或 `--output <path>`
- **THEN** 片段 SHALL 输出到 stdout 或指定文件，且不写组片段、不落盘私钥、不注册 Include

#### Scenario: 按组导出
- **WHEN** 执行 `senv host export --group prod`
- **THEN** 仅 `groups/prod.conf` SHALL 被重建，其余组片段不动

#### Scenario: 跨组 ProxyJump 闭包
- **WHEN** `prod` 组内 host 的 `proxyJump` 指向 `network` 组的 `bastion`，执行 `senv host export --group prod`
- **THEN** `bastion` 的 Host 块 SHALL 并入 `groups/prod.conf`，导出成功

#### Scenario: 组消失自动清理片段
- **WHEN** vault 中已无 `old` 组的任何 host，`groups/old.conf` 仍存在，执行全量 export
- **THEN** export SHALL 删除 `groups/old.conf` 并在摘要中说明；若执行的是 `--group prod` 单组导出，则 `groups/old.conf` MUST 保持不动

#### Scenario: IdentityFile 引用的 keypair 本机缺失
- **WHEN** 某 host 的 `identityKey` 指向本机 vault 不存在的 keypair（如档案尚未同步到本机）
- **THEN** export SHALL 逐条 warning 指明 host 别名与缺失 keypair 名，片段照常生成

#### Scenario: 落盘文件已存在
- **WHEN** 被引用 keypair 的落盘文件已存在于目标路径
- **THEN** export MUST 跳过该文件（不覆盖、不报错），摘要中标注 skipped

#### Scenario: 注册 Include 到既有 ssh config
- **WHEN** `~/.ssh/config` 已存在且有用户手写内容，但无 senv Include 行
- **THEN** export SHALL 在文件顶部插入一行 `Include ~/.ssh/senv/groups/*.conf`，其余内容原样保留，并留存 `.senv-bak`

#### Scenario: 未引用落盘私钥
- **WHEN** `~/.ssh/senv/keys/` 下存在未被任何 host `identityKey` 引用的私钥文件
- **THEN** export SHALL 逐条 warning 列出，MUST NOT 删除

#### Scenario: 组名撞保留名
- **WHEN** vault 中存在真实组名 `_ungrouped` 且有未分组 host/keypair
- **THEN** export SHALL 报错拒绝导出（冲突说明在错误信息中）

### Requirement: host 导出撤回（Unexport）
`senv host unexport` SHALL 幂等摘除 `~/.ssh/config` 中 senv 注册的 Include 行并删除 `~/.ssh/senv/groups/` 下全部组片段；MUST NOT 删除 vault 中的 host/keypair 档案，MUST NOT 删除 `~/.ssh/senv/keys/` 下的落盘私钥。注册行或组片段已不存在时 SHALL 正常完成并报告无变更。

#### Scenario: 撤回后本机无 senv 痕迹
- **WHEN** 已应用导出，执行 `senv host unexport`
- **THEN** `~/.ssh/config` 中的 senv Include 行被移除（其余内容不动），`groups/` 下组片段被删除，`keys/` 与 vault 档案原样保留

#### Scenario: 重复撤回幂等
- **WHEN** 再次执行 `senv host unexport`
- **THEN** 系统 SHALL 正常完成并报告无变更，不报错

#### Scenario: 未应用过导出
- **WHEN** 执行 `senv host unexport`，本无注册行与组片段
- **THEN** 系统 SHALL 报告无变更，不创建任何文件

### Requirement: keypair materialize 落盘
`senv keypair materialize <name>` SHALL 将私钥解密写入 `~/.ssh/senv/keys/<keypair 分组>/<name>`（未分组的分组目录为 `_ungrouped`；目录 0700、文件 0600）；目标已存在时默认 SHALL 拒绝覆盖（`--force` 覆盖）。

#### Scenario: Materialize to convention directory
- **WHEN** keypair `web-key` 的 group 为 `prod`，执行 `senv keypair materialize web-key`
- **THEN** `~/.ssh/senv/keys/prod/web-key` SHALL 存在且权限 0600，目录权限 0700，命令输出该路径

#### Scenario: 未分组落盘路径
- **WHEN** keypair `old-key` 无分组，执行 `senv keypair materialize old-key`
- **THEN** 私钥 SHALL 写入 `~/.ssh/senv/keys/_ungrouped/old-key`

#### Scenario: Refuse overwrite
- **WHEN** 目标文件已存在且未指定 `--force`
- **THEN** 系统 SHALL 报错拒绝覆盖

#### Scenario: Force overwrite
- **WHEN** 目标文件已存在且指定 `--force`
- **THEN** 系统 SHALL 覆盖为目标 keypair 的当前私钥内容

### Requirement: 未引用落盘私钥清理（prune）
`senv keypair prune` SHALL 列出 `~/.ssh/senv/keys/` 下未被任何 vault host `identityKey` 引用的私钥文件（标注对应 keypair 是否仍在 vault 中），经用户确认后删除；无显式确认 MUST NOT 删除任何文件。被引用 keypair 的落盘文件 MUST NOT 出现在清理清单。

#### Scenario: 清理未引用私钥
- **WHEN** `keys/` 下存在未引用文件，执行 `senv keypair prune` 并确认
- **THEN** 列出的文件被删除，被引用 keypair 的文件不动，vault 无变化

#### Scenario: 全部文件被引用
- **WHEN** `keys/` 下所有私钥均被 host 引用，执行 `senv keypair prune`
- **THEN** 系统 SHALL 提示无可清理项，不删除任何文件

#### Scenario: 未确认不删除
- **WHEN** 用户执行 `senv keypair prune` 后未确认
- **THEN** 系统 MUST NOT 删除任何文件
### Requirement: TUI 导出撤回与私钥清理

TUI SHALL 为两条 CLI 管理命令补齐入口，语义与 CLI 逐一等价。SSH Tab SHALL 提供 `u`（unexport）键：按键后系统 SHALL 先只读预检（`~/.ssh/config` 的 senv 注册行是否在位、`~/.ssh/senv/groups/` 下组片段数），无可撤回项时 toast 告知且不进确认框；否则 SHALL 弹确认框，逐项列出将发生的动作——移除 senv Include 注册行（仅在位时）、删除 N 个组片段（仅 fragments>0 时，N 实填）、并明示「`~/.ssh/senv/keys/` 下落盘私钥保留」；`enter/y` 确认后异步执行与 CLI `senv host unexport` 同一 `Manager.Unexport` 编排，`esc/n` 取消且零副作用。KeyPair Tab SHALL 提供 `p`（prune）键：按键后异步取候选清单，非空时 SHALL 先弹列表（逐条路径，vault 中仍有对应 keypair 的标注 `(keypair still in vault)`）再于同屏请求确认；`enter/y` 确认后异步删除与 CLI `senv keypair prune` 同一 `PruneCandidates`/`DeletePrunedFiles` 白名单集合，`esc/n` 取消且 MUST NOT 删除任何文件。两操作 MUST NOT 触碰 vault 档案；prune 的删除集合 MUST NOT 包含被任何 host 引用的落盘私钥。两入口执行后 SHALL 以 toast 报告实际结果（撤回了几项/删除了几个文件）并记录本机操作审计。

#### Scenario: SSH Tab 撤回已应用的导出
- **WHEN** 已应用导出（注册行在位、`groups/` 有 3 个片段），用户在 SSH Tab 按 `u`，确认框按 `y`
- **THEN** 系统 SHALL 异步执行 unexport：`~/.ssh/config` 的 senv Include 行被移除（其余内容不动并留 `.senv-bak`），3 个组片段被删除，`keys/` 下落盘私钥与 vault 档案原样保留，toast 报告实际撤回项并记 `op_ssh_host` 审计

#### Scenario: SSH Tab 无可撤回项
- **WHEN** 本无注册行且 `groups/` 无片段，用户在 SSH Tab 按 `u`
- **THEN** 系统 SHALL toast 告知 nothing to unexport，不弹确认框、不产生任何文件副作用

#### Scenario: SSH Tab 取消撤回
- **WHEN** 用户在 unexport 确认框按 `esc`/`n`
- **THEN** 系统 MUST NOT 改动 `~/.ssh/config` 与 `groups/`，返回 normal mode

#### Scenario: KeyPair Tab 清理未引用私钥
- **WHEN** `keys/` 下存在 2 个未被任何 host 引用的落盘私钥（其中 1 个对应 keypair 仍在 vault），用户在 KeyPair Tab 按 `p`，列表逐条展示路径与 `(keypair still in vault)` 标注，用户按 `y` 确认
- **THEN** 系统 SHALL 异步删除该 2 个文件并 toast `deleted 2 file(s)`，被引用 keypair 的落盘文件与 vault 均无变化，记 `op_ssh_keypair` 审计

#### Scenario: KeyPair Tab 无候选可清理
- **WHEN** 所有落盘私钥均被 host 引用，用户按 `p`
- **THEN** 系统 SHALL toast 提示无可清理项，不弹列表、不删除任何文件

#### Scenario: KeyPair Tab 取消清理
- **WHEN** 用户在 prune 列表确认屏按 `esc`/`n`
- **THEN** 系统 MUST NOT 删除任何文件，返回 normal mode

#### Scenario: prune 部分删除失败
- **WHEN** 确认删除后其中 1 个文件因权限缺失删除失败
- **THEN** 系统 SHALL 保留已删结果不回滚，以警告呈现 `deleted N-1 of N` 与错误原因，列表刷新反映实际删除并记失败审计

### Requirement: TUI 应用导出（Apply）

SSH Tab SHALL 提供 `A`（apply）键执行应用导出，与 CLI `senv host export` 同一 `Manager.Apply` 编排：host 栏聚焦时重建当前 host 所在组的整组片段；分组侧栏聚焦时重建选中组（「All」伪组 = 全量重建并清理幽灵组片段）。执行前 SHALL 弹确认框，列出将写入的组片段数、待落盘私钥数、Include 注册状态与 warning 计数；`enter/y` 确认执行，`esc/n` 取消且不产生任何副作用。结果 SHALL 以摘要 toast 呈现（重建/落盘/跳过/注册各项计数）。批量导出（`x` 多选）的输出目录表单与单条导出（预览后 `w`）的目标文件表单 MUST 拒绝 `~/.ssh/senv/` 内部路径（含 `groups/` 与 `keys/`），报错提示改用 `A` 应用导出——该目录树由 apply 全权拥有，外来文件会被幽灵清理删除。

#### Scenario: host 栏 apply 重建所在组
- **WHEN** 用户在 host 栏选中 host `web`（属组 `prod`）按 `A`，确认框列出 1 个组片段与待落盘密钥数，用户按 `y`
- **THEN** 系统 SHALL 调 `Apply({Host: "web"})` 重建 `groups/prod.conf`、落盘缺失私钥并保证 Include 注册，toast 展示摘要

#### Scenario: 侧栏按组 apply
- **WHEN** 用户在分组侧栏选中组 `prod` 按 `A` 并确认
- **THEN** 系统 SHALL 调 `Apply({Group: "prod"})` 只重建 `groups/prod.conf`，其余组片段不动

#### Scenario: 侧栏 All 全量 apply
- **WHEN** 用户在侧栏选中「All」按 `A` 并确认
- **THEN** 系统 SHALL 调 `Apply({})` 全量重建全部组片段并清理 vault 中已消失组的幽灵片段

#### Scenario: 取消无副作用
- **WHEN** 用户在确认框按 `esc`/`n`
- **THEN** 系统 MUST NOT 写任何组片段、私钥或 ssh config

#### Scenario: 批量导出目录防护
- **WHEN** 用户在批量导出表单的输出目录填入 `~/.ssh/senv/groups` 或 `~/.ssh/senv` 下任意路径并提交
- **THEN** 表单 SHALL 内联报错拒绝，提示该目录由 `A` 应用导出维护，应改用用户自有目录

#### Scenario: 单条导出目标文件防护
- **WHEN** 用户在单条导出的写文件表单中把目标文件填进 `~/.ssh/senv/` 内任意位置
- **THEN** 表单 SHALL 内联报错拒绝，提示改用 `A` 应用导出或另选用户自有路径

#### Scenario: 确认框呈现 warning 计数
- **WHEN** 待导出的 host 引用了本机 vault 缺失的 keypair，或落盘上存在未引用私钥
- **THEN** 确认框 SHALL 显示 warning 计数（明细在 CLI `senv host export` 可见），执行后 toast 保留该计数

### Requirement: TUI 编辑与 MCP 只读集成

TUI SHALL 提供 host 与 keypair 的完整编辑板块，分属两个 Tab：SSH Tab 负责浏览、新建、编辑、删除 host 与导出 OpenSSH 片段；KeyPair Tab（独立 Tab，紧随 SSH Tab）负责导入、重命名、删除、materialize keypair 及分组管理。私钥内容 SHALL 始终遮蔽（仅展示指纹/公钥摘要），明文 MUST NOT 进入 TUI 状态或渲染输出。host 列表 SHALL 显示其关联的 keypair 名称与指纹摘要。编辑 host 时关联 keypair SHALL 通过选择器完成，引用的 keypair 不存在时 MUST 拒绝保存。MCP SHALL 保持只读：提供 host 列表/详情工具，MUST NOT 提供返回私钥明文的工具。

#### Scenario: TUI masks private key
- **WHEN** 在 TUI 中查看 keypair `web-key`
- **THEN** 界面 SHALL 显示指纹与元信息，私钥内容 SHALL 遮蔽

#### Scenario: MCP read-only host query
- **WHEN** agent 通过 MCP 查询 host 列表
- **THEN** 返回 SHALL 包含连接信息与指纹，MUST NOT 包含私钥明文

#### Scenario: TUI 新建 host 并关联 keypair
- **WHEN** 用户在 SSH Tab 新建 host，选择已有 keypair 作为身份密钥并提交
- **THEN** host 保存成功，列表中该 host 显示所关联的 keypair 名称与指纹摘要

#### Scenario: 引用不存在的 keypair 被拒绝
- **WHEN** 用户在 host 表单提交一个指向不存在 keypair 的引用
- **THEN** 表单内联报错，不写入任何数据

#### Scenario: TUI 删除被引用的 keypair
- **WHEN** 用户在 KeyPair Tab 对仍被 host 引用的 keypair 触发删除
- **THEN** 界面列出引用它的 host 并拒绝删除；用户显式确认强制删除后，这些 host 的 `identityKey` 被清空

#### Scenario: TUI 导出 host 片段
- **WHEN** 用户在 SSH Tab 触发导出 OpenSSH 片段
- **THEN** 先展示将写入的内容预览，确认后写目标文件；存在悬空 `proxyJump` 时按既有规则报错

### Requirement: TUI materialize 需确认
TUI 的 KeyPair materialize 确认框 SHALL 显示分组落盘路径 `~/.ssh/senv/keys/<分组>/<名>`（未分组为 `_ungrouped`，目标权限 0600）；目标已存在时需再次确认；成功后只提示路径，不渲染私钥内容。

#### Scenario: TUI materialize 需确认
- **WHEN** 用户在 KeyPair Tab 对 group 为 `prod` 的 keypair 触发 materialize
- **THEN** 显示确认框与目标路径 `~/.ssh/senv/keys/prod/<名>`（0600）；目标已存在时需再次确认；成功后只提示路径，不渲染私钥内容

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

### Requirement: TUI SSH 分组侧栏
TUI SSH Tab SHALL 以三栏呈现：分组侧栏 → 组内 Host 列表 → KeyPair 列表，与 Env/Text/Config 的分组侧栏交互一致。侧栏 SHALL 包含「All」伪组置顶、各组按组名字母序展示条目数、空 group 的 Host 归入「未分组」组置底且仅在有未归类 Host 时出现。组内 Host 按别名字典序排列。选中组决定中间栏展示的 Host 集合；「All」展示全部。

#### Scenario: 按组浏览 Host
- **WHEN** 用户在侧栏选中组 `prod`
- **THEN** 中间栏 SHALL 仅展示 `group: prod` 的 Host，按别名排序

#### Scenario: All 伪组
- **WHEN** 用户在侧栏选中「All」
- **THEN** 中间栏 SHALL 展示全部 Host，分组状态不被改变

#### Scenario: 未分组兜底
- **WHEN** 存在未设置 group 的 Host
- **THEN** 侧栏 SHALL 在字母序组之后显示「未分组」组；不存在未归类 Host 时该组 MUST NOT 出现

#### Scenario: 全部 Host 均有分组
- **WHEN** 所有 Host 均设置了非空 group
- **THEN** 侧栏 SHALL 不显示「未分组」
### Requirement: TUI Host 行内 tags 展示
Host 列表行 SHALL 在行尾展示 tags，格式为 `#tag` 前缀，最多 2 个，超出以 `+n` 汇总；行宽不足时 MUST 截断且不得折行。行内 MUST NOT 展示 group（group 由侧栏表达）。

#### Scenario: 有 tags 的 Host
- **WHEN** host `web` 的 tags 为 `[gpu, p0]`
- **THEN** 列表行 SHALL 在行尾展示 `#gpu #p0`

#### Scenario: tags 超过 2 个
- **WHEN** host `web` 的 tags 为 `[a, b, c, d]`
- **THEN** 行 SHALL 展示 `#a #b +2`

#### Scenario: 无 tags
- **WHEN** host `web` 未设置 tags
- **THEN** 行 SHALL 不渲染 tags 片段
### Requirement: SSH 过滤与全局搜索匹配范围
SSH Tab 的 `/` 过滤 SHALL 匹配 alias、hostname、tags 与 group 四个维度。全局搜索（`S`）在 SSH 类目下 SHALL 匹配 alias、hostname、tags 与 group。

#### Scenario: 按 tag 过滤
- **WHEN** 用户在 Host 栏输入 `/gpu`
- **THEN** tags 含 `gpu` 的 Host SHALL 保留，其余过滤掉

#### Scenario: 按 group 过滤
- **WHEN** 用户在 Host 栏输入 `/prod`
- **THEN** `group: prod` 的 Host SHALL 保留

#### Scenario: 全局搜索命中 group
- **WHEN** 用户按 `S` 搜索 `prod`
- **THEN** SSH 类目的结果 SHALL 包含 `group: prod` 的 Host

#### Scenario: 全局搜索命中 tag
- **WHEN** 用户按 `S` 搜索 `gpu`
- **THEN** SSH 类目的结果 SHALL 包含 tags 含 `gpu` 的 Host

### Requirement: KeyPair 分组字段
`KeyPairEntry` SHALL 包含单值 `group` 字段（空 = 未分组），语义与 Host 的 `group` 一致：组织维度，同时参与落盘与导出 `IdentityFile` 路径推导（`~/.ssh/senv/keys/<group>/<名>`）；group 值禁止包含 `/`，写入与编辑时校验。既有数据（无 `group` 字段）SHALL 按未分组处理，读写兼容。CLI SHALL 提供 `senv keypair edit <name> --group <group>` 作为分组编辑入口（`--group` 显式变更时单字段更新、不启编辑器；空值 = 清除分组），与 TUI KeyPair Tab `e` 等价；未显式变更 `--group` 时命令 SHALL 报错拒绝执行。

#### Scenario: 既有 keypair 无 group 字段
- **WHEN** 读取旧版本写入的 keypair（无 `group` 字段）
- **THEN** 系统 SHALL 按空 group（未分组）处理，读写均正常

#### Scenario: 分组名含路径分隔符
- **WHEN** `senv keypair import web-key --file ... --group a/b` 或编辑为含 `/` 的组名
- **THEN** 系统 SHALL 报错拒绝写入

#### Scenario: CLI 编辑 keypair 分组
- **WHEN** 用户执行 `senv keypair edit web-key --group prod`
- **THEN** keypair `web-key` 的 group SHALL 更新为 `prod`，`senv keypair list` 输出展示该分组，私钥与公钥材料 MUST NOT 变动

#### Scenario: CLI 清除 keypair 分组
- **WHEN** 用户执行 `senv keypair edit web-key --group ""`
- **THEN** group SHALL 置空（未分组），后续 materialize 落盘路径按 `_ungrouped` 推导

#### Scenario: CLI 编辑为非法分组名
- **WHEN** 用户执行 `senv keypair edit web-key --group a/b`
- **THEN** 系统 SHALL 报错拒绝写入，原 group 保持不变

#### Scenario: CLI 编辑不存在的 keypair
- **WHEN** 用户执行 `senv keypair edit nope --group prod`，keypair `nope` 不存在
- **THEN** 系统 SHALL 报错且不产生任何存储变更

#### Scenario: CLI 未指定 --group
- **WHEN** 用户执行 `senv keypair edit web-key`（未显式变更 `--group`）
- **THEN** 系统 SHALL 报错提示用法，不进入编辑器、不产生任何存储变更

### Requirement: TUI KeyPair Tab 分组侧栏
TUI SHALL 提供独立 KeyPair Tab（`mgr.SSH` 非空时注册，紧随 SSH Tab），布局为「分组侧栏 → KeyPair 列表」两栏，与 Env/Text/Config 分组交互一致。侧栏 SHALL 包含「All」伪组置顶、各组按组名字母序展示条目数、空 group 的 KeyPair 归入「未分组」组置底且仅在有未归类 KeyPair 时出现。列表内 KeyPair 按名字典序排列，行内 SHALL 展示指纹摘要与被 Host 引用计数（`被 N 个 Host 引用`）；零引用的 KeyPair SHALL 灰显「未被引用」，引用计数 MUST NOT 改变排序。`/` SHALL 按名称过滤。

#### Scenario: 按组浏览 KeyPair
- **WHEN** 用户在侧栏选中组 `prod`
- **THEN** 列表 SHALL 仅展示 `group: prod` 的 KeyPair，按名称排序

#### Scenario: 未分组兜底
- **WHEN** 存在未设置 group 的 KeyPair
- **THEN** 侧栏 SHALL 在字母序组之后显示「未分组」组；不存在未归类 KeyPair 时该组 MUST NOT 出现

#### Scenario: 引用计数与零引用
- **WHEN** keypair `web-key` 被 3 个 Host 引用，keypair `old-key` 未被引用
- **THEN** `web-key` 行 SHALL 展示 `被 3 个 Host 引用`，`old-key` 行 SHALL 灰显「未被引用」，两行均按名字序排列

#### Scenario: 按名称过滤
- **WHEN** 用户输入 `/web`
- **THEN** 列表 SHALL 仅保留名称匹配的 KeyPair
### Requirement: SSH Tab 两栏呈现
SSH Tab SHALL 为「分组侧栏 → Host 列表」两栏，不再内嵌 KeyPair 栏；Host 行 SHALL 保留内联的关联 keypair 名称与指纹摘要（`key:keyName(fp)`）。Host 栏 `/` 过滤的匹配范围（alias、hostname、tags、group）不变。

#### Scenario: Host 行内联 keypair 引用
- **WHEN** host `web` 关联 keypair `web-key`（指纹 fp）
- **THEN** Host 行 SHALL 展示 `key:web-key(fp)` 片段，KeyPair 详情不在本 Tab
