## MODIFIED Requirements

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

#### Scenario: TUI materialize 需确认
- **WHEN** 用户在 KeyPair Tab 对 keypair 触发 materialize
- **THEN** 显示确认框与目标路径 `~/.ssh/senv/<name>`（0600）；目标已存在时需再次确认；成功后只提示路径，不渲染私钥内容

#### Scenario: TUI 导出 host 片段
- **WHEN** 用户在 SSH Tab 触发导出 OpenSSH 片段
- **THEN** 先展示将写入的内容预览，确认后写目标文件；存在悬空 `proxyJump` 时按既有规则报错

## ADDED Requirements

### Requirement: KeyPair 分组字段
`KeyPairEntry` SHALL 包含单值 `group` 字段（空 = 未分组），语义与 Host 的 `group` 一致：纯组织维度，不参与 materialize 与导出。既有数据（无 `group` 字段）SHALL 按未分组处理，读写兼容。

#### Scenario: 既有 keypair 无 group 字段
- **WHEN** 读取旧版本写入的 keypair（无 `group` 字段）
- **THEN** 系统 SHALL 按空 group（未分组）处理，读写均正常

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

## REMOVED Requirements

### Requirement: KeyPair 栏引用计数与过滤
**Reason**: KeyPair 升级为独立 Tab，引用计数、零引用提示与过滤由新 Requirement「TUI KeyPair Tab 分组侧栏」承接
**Migration**: 行为不变，呈现位置从 SSH Tab 右栏迁至 KeyPair Tab
