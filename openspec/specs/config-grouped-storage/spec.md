# config-grouped-storage Specification

## Purpose
config 私密配置的分组存储模型：每条配置归属一个分组并携带 meta（描述、保存位置），内容加密存储，保存位置在使用时做 `~` 与环境变量展开。
## Requirements
### Requirement: 分组与 meta 模型
每条配置 SHALL 具有 group（分组）与 meta 信息，meta 至少包含 description（描述）与 target path（保存位置）。未指定 group 时 SHALL 落入 `default` 分组。配置 name SHALL 全局唯一，group 仅作为分类维度。

#### Scenario: 创建带分组与描述的配置
- **WHEN** 创建配置时指定 name、group、description、target path
- **THEN** 该配置以加密形式持久化，索引中记录其 group、description 与 target path 原始写法

#### Scenario: 未指定分组
- **WHEN** 创建配置时未指定 group
- **THEN** 该配置归属 `default` 分组

### Requirement: 向后兼容
旧格式索引（无 group/description 字段）SHALL 被正常读取，其中配置视为 `default` 分组、空描述，且读取操作 SHALL NOT 强制改写索引文件。

#### Scenario: 读取旧索引
- **WHEN** 加载由旧版本写入的 config 索引
- **THEN** 所有配置以 `default` 分组、空描述正常列出，无报错

### Requirement: 保存位置展开
target path 中的 `~` SHALL 展开为当前用户主目录，`$VAR` 与 `${VAR}` 形式的环境变量 SHALL 在使用时展开。展开 SHALL 发生在使用路径的操作（如 install/export）执行时，而非写入存储时。

#### Scenario: 展开 home 与变量
- **WHEN** target path 为 `~/.config/$APP_NAME/config.yaml` 且环境变量 `APP_NAME` 已设置
- **THEN** 使用时解析为主目录下对应绝对路径

#### Scenario: 引用未定义变量
- **WHEN** target path 引用的环境变量未设置
- **THEN** 操作在计划阶段报告该错误，不执行任何写操作

### Requirement: 分组查询
列表与查询操作 SHALL 支持按 group 过滤，并展示每条配置的 description 与 target path。

#### Scenario: 按组列出
- **WHEN** 按 group 列出配置
- **THEN** 仅返回该分组下的配置及其 meta 信息

### Requirement: config 名称与分组必须是安全身份

config name 与 group SHALL 为非空安全单路径段，不得为 `.`、`..`，也不得包含 NUL、`:`、`/`、`\\`、绝对路径或平台卷标语义。校验 MUST 在 manager 与 storage 边界执行，并覆盖 create、read、load、list、edit、export、install 和 delete。

#### Scenario: create 拒绝穿越名称
- **WHEN** 用户或 MCP 以 `../escaped`、绝对路径或含分隔符的 name 创建 config
- **THEN** 操作在读取源内容或写入索引前失败，data 根外不产生 `.enc` 文件

#### Scenario: delete 拒绝非法名称
- **WHEN** delete 收到 `.`、`..`、空值或含路径语义的 name
- **THEN** 系统删除零个文件并返回非法名称错误

#### Scenario: 合法 config 身份
- **WHEN** name 为 `database-prod` 且 group 为 `prod`
- **THEN** 配置仍按既有全局唯一名称与分组模型存储

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

### Requirement: 损坏索引不得驱动破坏性操作

若 config index 任一记录身份无效，依赖该索引的 edit、export、install、delete、rekey 或同步操作 MUST 在文件系统变更前失败，不得跳过坏记录后继续。

#### Scenario: rekey 遇到坏索引
- **WHEN** rekey 加载到包含非法 `EncryptedFile` 的 config index
- **THEN** rekey 预检失败，metadata 与全部密文保持原样

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

### Requirement: config 说明上限
config 条目已有的 description SHALL 遵守 2048 字节上限；超限的 create 或 SetMeta MUST 拒绝且不改索引。list/get 已展示 description 的行为保持不变。

#### Scenario: oversize config description
- **WHEN** 用户以超过 2048 字节的 description 创建或修改 config
- **THEN** 操作失败，不写入该 description

