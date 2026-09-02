## Purpose

将 PBKDF2 迭代次数等密钥派生参数纳入 metadata 版本化管理，使口令到密钥的计算成本可随安全基线升级，并通过既有 rekey 流程平滑迁移存量 vault，同时保持旧格式可读。

## ADDED Requirements

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
