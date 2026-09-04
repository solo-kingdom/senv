## ADDED Requirements

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
