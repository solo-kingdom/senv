## Purpose

将 PBKDF2 迭代次数等密钥派生参数纳入 metadata 版本化管理，使口令到密钥的计算成本可随安全基线升级，并通过既有 rekey 流程平滑迁移存量 vault，同时保持旧格式可读。

## Requirements

### Requirement: KDF 参数随 metadata 版本化

加密元数据 SHALL 记录生成其所用的 PBKDF2 迭代次数（`kdf_iterations`）；该字段缺失或为 0 时，系统 SHALL 按 100,000 次解释（向后兼容旧格式）。所有使用口令派生密钥的路径（解锁校验、数据解密、会话派生）MUST 使用与 metadata 记录一致的迭代次数。

#### Scenario: 旧格式 vault 正常解锁

- **WHEN** 打开一个不含 `kdf_iterations` 字段的既有 vault 并输入正确口令
- **THEN** 系统按 100,000 次迭代派生并解锁成功，不要求立即迁移

#### Scenario: 新格式 vault 使用记录的迭代次数

- **WHEN** 打开一个 `kdf_iterations` 为 600000 的 vault 并输入正确口令
- **THEN** 系统按 600,000 次迭代派生并解锁成功

### Requirement: 新建与 rekey 使用强化参数

新建 vault SHALL 以 600,000 次迭代（OWASP 2026 对 PBKDF2-SHA256 的建议值）派生并写入 `kdf_iterations`；既有 vault 经口令变更/rekey 后 SHALL 同步升级到 600,000 次，并重加密全部数据文件与重算 passwordKey。

#### Scenario: 新建 vault 记录强化参数

- **WHEN** 用户初始化一个新 vault
- **THEN** metadata 中 `kdf_iterations` 为 600000

#### Scenario: rekey 升级存量 vault

- **WHEN** 用户对 100,000 次迭代的既有 vault 执行 `senv passwd` 成功
- **THEN** 全部数据文件以新密钥重加密，metadata 的 `kdf_iterations` 变为 600000，旧口令不再能解锁

### Requirement: KDF 参数公开不降低安全性

KDF 参数属于公开成本参数，系统 MAY 将其在 metadata 中明文存储；迭代次数的升级 MUST NOT 改变加密算法本体（仍为 AES-256-GCM）、盐长度（32 字节）与派生输出长度（256 位）。

#### Scenario: 参数字段不承载机密

- **WHEN** 任意用户读取 metadata 文件内容
- **THEN** 其中仅含盐、passwordKey 与 KDF 参数，不含口令、派生钥或任何明文数据

### Requirement: KDF 迭代参数在派生前受版本边界约束

系统 SHALL 在调用 PBKDF2 前按 metadata 版本验证 `kdf_iterations`。当前格式 MUST 将缺失或 `0` 解释为 legacy 100,000，并仅接受 100,000 至 1,000,000（含边界）的显式值；负数、过低值、超上限值及无法表示的值 MUST 作为不受支持或损坏的 metadata fail fast。未来 metadata 版本可定义新的边界，但 MUST 在执行成本函数前完成验证。

#### Scenario: legacy 零值继续兼容
- **WHEN** metadata 缺失 `kdf_iterations` 或该值为 `0`
- **THEN** 系统按 100,000 次派生并可正常解锁旧 vault

#### Scenario: 当前默认值被接受
- **WHEN** metadata 的 `kdf_iterations` 为 600,000
- **THEN** 系统执行 600,000 次 PBKDF2 并按既有流程验证口令

#### Scenario: 过低或负数被拒绝
- **WHEN** metadata 显式提供负数或小于 100,000 的非零值
- **THEN** 系统在 PBKDF2 前返回 KDF 参数无效错误，不将其误报为密码错误

#### Scenario: 极大值被快速拒绝
- **WHEN** metadata 提供大于 1,000,000 或平台最大整数的迭代值
- **THEN** 系统不执行该成本的 PBKDF2，快速返回不受支持的 KDF 参数错误

#### Scenario: 同步得到恶意 metadata
- **WHEN** server 或仓库同步下来的 metadata 含超上限 KDF 参数
- **THEN** 客户端拒绝使用其派生 key，不发生长时间 CPU 占用，并保留可诊断错误

### Requirement: 所有口令派生入口使用同一验证结果

解锁、密码验证、session start、rekey 预检及任何从 metadata 派生 key 的入口 MUST 采用同一版本化 KDF 参数校验，任何入口不得绕过上限或自行回退到默认值。

#### Scenario: session start 遇到恶意成本
- **WHEN** metadata 的迭代值超出允许边界且用户执行 `session start`
- **THEN** 命令在提示后的密钥派生前失败，不创建 session cache

#### Scenario: 不同入口结果一致
- **WHEN** 同一份非法 metadata 分别经 CLI 解锁、MCP 启动和 rekey 访问
- **THEN** 所有入口均返回同类参数错误且不执行超界 PBKDF2
