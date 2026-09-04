## MODIFIED Requirements

### Requirement: config index 身份映射必须一致

加载 config index 时，系统 SHALL 分别验证 map key、记录中的 `Name`、`Group` 与 `EncryptedFile`。map key 与 `Name` MUST 相同；非空 `EncryptedFile` MUST 精确等于该 name 的规范密文文件名且为单一文件名。为兼容旧索引，空 `EncryptedFile` SHALL 仅在内存中解释为规范文件名，不强制改写索引。

身份校验 SHALL 区分两类结果：

1. **结构性无效**：map key 与 `Name` 不一致、`EncryptedFile` 与规范名不匹配、或任何字段含路径穿越（`..`、`/`、`\` 绝对路径、卷标、NUL）语义。此类记录 MUST 使索引加载整体 fail closed，任何路径（含只读）不得跳过后继续。
2. **仅不可移植**：各字段之间结构一致，不含穿越语义，仅因包含 `:` 等可移植性字符被单段身份规则拒绝的存量记录。此类记录在**只读**加载（list、groups、TUI 展示、一致性探针）中 SHALL 被隔离跳过并随结果返回可展示的警告，不使整个索引加载失败；在破坏性操作中仍按无效记录处理。

#### Scenario: map key 与 Name 不一致

- **WHEN** index map key 为 `db` 但记录的 `Name` 为 `other`
- **THEN** 索引加载失败并报告身份不一致，不读取或删除任一候选密文

#### Scenario: EncryptedFile 指向根外

- **WHEN** `EncryptedFile` 为 `../escaped.enc`、绝对路径或与 name 不匹配的文件名
- **THEN** 索引加载 fail closed，根外文件保持不变

#### Scenario: legacy 空 EncryptedFile

- **WHEN** 合法旧索引记录的 `EncryptedFile` 为空
- **THEN** 系统将其解释为 `<name>.enc` 并正常读取，且仅只读操作不改写原索引

#### Scenario: legacy 冒号名在只读路径被隔离

- **WHEN** 索引含记录 `feg:ai-ops-portal.pub`（key、`Name`、`EncryptedFile` 结构一致，仅名称含 `:`），用户运行 `config list`
- **THEN** 其余合法配置正常列出，该条目被跳过并输出包含原名的隔离警告，命令不因该条目失败

#### Scenario: 结构性无效不得被隔离

- **WHEN** 索引某记录的 `EncryptedFile` 为 `../escaped.enc`，用户运行 `config list`
- **THEN** 列表整体失败并报告结构性无效，不得跳过该记录返回其余条目

## ADDED Requirements

### Requirement: 索引缺失视为空索引

config index 文件不存在（`ErrNotExist`）时，索引加载 SHALL 返回空索引而非错误。空库的只读操作（如 `config list`）SHALL 正常返回空结果；创建首条配置 SHALL 正常成功；对不存在的配置执行删除或查询 SHALL 报告不存在，而非索引加载错误。

#### Scenario: 全新空库列出配置

- **WHEN** config 目录中不存在 `config_index.json`，用户运行 `config list`
- **THEN** 命令成功返回空列表，不输出 "failed to load config index"

#### Scenario: 全新空库创建首条配置

- **WHEN** 索引文件不存在，用户创建首条配置
- **THEN** 创建成功并写入包含该条目的新索引

#### Scenario: 空库删除不存在的配置

- **WHEN** 索引文件不存在，用户删除任意配置
- **THEN** 返回 "config not found" 类错误，而非索引加载错误

### Requirement: 存量非法名称的只读隔离

当索引同时包含合法条目与"仅不可移植"的存量条目时，list、groups、TUI 配置页与一致性探针 SHALL 返回全部合法条目，并为每条被隔离条目输出可见警告（至少包含原名称与 "config repair" 修复指引）。只读路径 MUST NOT 打开被隔离条目对应的密文文件。

#### Scenario: TUI 配置页带警告加载

- **WHEN** 索引同时含 `database-prod`（合法）与 `feg:ai-ops-portal.pub`（仅不可移植），打开 TUI 配置页
- **THEN** 页面列出 `database-prod`，警告栏提示 `feg:ai-ops-portal.pub` 已隔离并可运行 `senv config repair`

#### Scenario: 全部条目非法时只读返回空

- **WHEN** 索引仅含一条"仅不可移植"条目，运行 `config list`
- **THEN** 命令成功返回空列表并输出该条目的隔离警告

### Requirement: config repair 安全改写

系统 SHALL 提供 `senv config repair` 命令，将索引中"仅不可移植"的存量条目改写为可移植名称。命令 SHALL：

- 列出每条待修复条目及其建议新名称（确定性改写规则，改写结果 MUST 通过单段身份校验且不与现有任何条目冲突，冲突时 MUST 失败而非猜测改名）
- 在执行任何变更前获得用户确认（非交互环境提供显式跳过确认的选项）
- 在同一变更锁内原子性更新索引中的 map key、`Name`、`EncryptedFile` 并重命名对应密文文件
- 对密文文件缺失的陈旧条目，默认报告错误并拒绝执行；仅在用户显式选择丢弃选项时才从索引中移除该条目
- 全程不输出任何明文内容

#### Scenario: 修复含冒号的存量名

- **WHEN** 索引含 `feg:ai-ops-portal.pub` 且对应 `.enc` 文件存在，用户运行 `config repair` 并确认建议名称
- **THEN** 索引与密文文件均改用可移植新名，随后 `config list` 不再出现隔离警告，新名条目可正常导出

#### Scenario: 建议名称冲突时拒绝

- **WHEN** 建议新名称与另一条现有配置重名
- **THEN** repair 在任何写入前失败并报告冲突，索引与文件保持原样

#### Scenario: 陈旧条目默认拒绝修复

- **WHEN** 待修复条目的密文文件不存在且用户未选择丢弃选项
- **THEN** repair 报告缺失并拒绝执行，不移除索引条目
