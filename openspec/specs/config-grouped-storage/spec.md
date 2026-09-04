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

#### Scenario: map key 与 Name 不一致
- **WHEN** index map key 为 `db` 但记录的 `Name` 为 `other`
- **THEN** 索引加载失败并报告身份不一致，不读取或删除任一候选密文

#### Scenario: EncryptedFile 指向根外
- **WHEN** `EncryptedFile` 为 `../escaped.enc`、绝对路径或与 name 不匹配的文件名
- **THEN** 索引加载 fail closed，根外文件保持不变

#### Scenario: legacy 空 EncryptedFile
- **WHEN** 合法旧索引记录的 `EncryptedFile` 为空
- **THEN** 系统将其解释为 `<name>.enc` 并正常读取，且仅只读操作不改写原索引

### Requirement: 损坏索引不得驱动破坏性操作

若 config index 任一记录身份无效，依赖该索引的 edit、export、install、delete、rekey 或同步操作 MUST 在文件系统变更前失败，不得跳过坏记录后继续。

#### Scenario: rekey 遇到坏索引
- **WHEN** rekey 加载到包含非法 `EncryptedFile` 的 config index
- **THEN** rekey 预检失败，metadata 与全部密文保持原样
