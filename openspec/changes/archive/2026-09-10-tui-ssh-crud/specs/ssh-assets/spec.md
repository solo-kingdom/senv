# ssh-assets Delta

## RENAMED Requirements

- FROM: `### Requirement: TUI 浏览与 MCP 只读集成`
- TO: `### Requirement: TUI 编辑与 MCP 只读集成`

## ADDED Requirements

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

## MODIFIED Requirements

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
