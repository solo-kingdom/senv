# server-auth Specification

## MODIFIED Requirements

### Requirement: Bearer token 认证

除健康检查与用户创建外的所有 API SHALL 要求 `Authorization: Bearer <token>`。token 无效、缺失或已吊销时 MUST 返回 401，MUST NOT 泄露 token 是否存在的信息。server MUST 只存储 token 的哈希，MUST NOT 存储或可反推明文 token。配置 pepper（`SENV_SERVER_TOKEN_PEPPER`）时哈希 MUST 为 `HMAC-SHA256(pepper, token)` 且 pepper MUST NOT 入库；未配置时 MUST 保持原 SHA-256 行为不变。启用 pepper 后，存量 SHA-256 哈希 token MAY 经一次性回退比对继续认证，回退命中 SHOULD 产生轮换提示日志；回退路径 MUST NOT 改变响应语义或产生可观测时序差异。token 哈希算法 MUST 在 serve 与 admin CLI 两侧同源：两者 MUST 以同一方式读取该环境变量，否则 admin 侧按裸 SHA-256 计算将无法命中 HMAC 存储的 token（吊销静默失效），且其新签 token 会绕过 pepper。

#### Scenario: 无 token 访问
- **WHEN** 请求受保护接口且未携带 token
- **THEN** 返回 401，响应体不含任何用户/vault 信息

#### Scenario: 吊销后失效
- **WHEN** 某 token 被吊销后用其访问接口
- **THEN** 返回 401，与该 token 从未存在过的响应一致

#### Scenario: pepper 下新 token 认证
- **WHEN** 配置 pepper 后创建用户并携新 token 访问
- **THEN** 认证通过，库中仅存 HMAC 哈希，pepper 不出现在库与日志中

#### Scenario: 存量 token 兼容
- **WHEN** 启用 pepper 后旧 SHA-256 token 首次访问
- **THEN** 回退比对通过，记轮换提示日志，响应与 HMAC token 路径一致

#### Scenario: 认证语义不泄露
- **WHEN** 用任意无效 token（无论哈希时代）访问
- **THEN** 统一 401，无存在性/哈希时代差异

#### Scenario: admin 侧与 serve 侧哈希同源
- **WHEN** 配置 pepper 的部署里执行 `admin revoke-token`（明文 token 以 HMAC 存储）
- **THEN** 命中并吊销该 token，而非报「不存在」；同一环境下 `admin create-user` 新签 token 以 HMAC 哈希入库
