## MODIFIED Requirements

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

### Requirement: KeyPair 分组字段
`KeyPairEntry` SHALL 包含单值 `group` 字段（空 = 未分组），语义与 Host 的 `group` 一致：组织维度，同时参与落盘与导出 `IdentityFile` 路径推导（`~/.ssh/senv/keys/<group>/<名>`）；group 值禁止包含 `/`，写入与编辑时校验。既有数据（无 `group` 字段）SHALL 按未分组处理，读写兼容。

#### Scenario: 既有 keypair 无 group 字段
- **WHEN** 读取旧版本写入的 keypair（无 `group` 字段）
- **THEN** 系统 SHALL 按空 group（未分组）处理，读写均正常

#### Scenario: 分组名含路径分隔符
- **WHEN** `senv keypair import web-key --file ... --group a/b` 或编辑为含 `/` 的组名
- **THEN** 系统 SHALL 报错拒绝写入

### Requirement: TUI materialize 需确认
TUI 的 KeyPair materialize 确认框 SHALL 显示分组落盘路径 `~/.ssh/senv/keys/<分组>/<名>`（未分组为 `_ungrouped`，目标权限 0600）；目标已存在时需再次确认；成功后只提示路径，不渲染私钥内容。

#### Scenario: TUI materialize 需确认
- **WHEN** 用户在 KeyPair Tab 对 group 为 `prod` 的 keypair 触发 materialize
- **THEN** 显示确认框与目标路径 `~/.ssh/senv/keys/prod/<名>`（0600）；目标已存在时需再次确认；成功后只提示路径，不渲染私钥内容

## ADDED Requirements

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
