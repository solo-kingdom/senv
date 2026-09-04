## ADDED Requirements

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
