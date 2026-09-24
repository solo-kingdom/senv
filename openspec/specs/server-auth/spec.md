# server-auth Specification

## Purpose
定义 senv-server 的多用户身份与访问控制：用户持有 Bearer token 访问自己的 vault，server 只存 token 哈希，vault 加密口令永不经过 server。
## Requirements
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

### Requirement: 用户与 token 管理

系统 SHALL 提供创建用户并签发 token 的管理入口（CLI 子命令），创建时明文 token 只展示一次。系统 SHALL 支持吊销指定 token 且不影响同用户其他 token。

#### Scenario: 创建用户

- **WHEN** 管理员执行用户创建命令
- **THEN** 输出一次性明文 token，库中仅保存其哈希

### Requirement: vault 隔离

用户 MUST 只能访问自己名下的 vault 与其条目；跨用户访问 MUST 返回 404（而非 403），不泄露 vault 存在性。

#### Scenario: 跨用户访问 vault

- **WHEN** 用户 A 用有效 token 请求用户 B 的 vault
- **THEN** 返回 404，与 vault 不存在时的响应一致

### Requirement: vault 口令不经 server

任何 API MUST NOT 接收、记录或存储 vault 加密口令或其派生 key。salt 与 passwordKey 仅作为不透明密文 blob 存储与透传。

#### Scenario: metadata 透传

- **WHEN** 客户端读写 vault metadata
- **THEN** server 原样存储/返回 blob，不做任何解析或校验其内容

### Requirement: 认证失败限速

server 对认证失败的请求 SHALL 按来源实施限速（固定窗口计数即可）；超限来源的后续请求 SHALL 返回 429，且响应 MUST 与 vault 不存在时一致地不泄露任何账户或 vault 存在性信息。限速状态 MAY 保存在内存中，进程重启后清零。

#### Scenario: 连续失败触发限速

- **WHEN** 同一来源在窗口内连续提交错误 token 超过阈值
- **THEN** 后续请求返回 429，server 侧记录失败计数

#### Scenario: 正常用户不被误伤

- **WHEN** 持有效 token 的用户在窗口内正常读写（未触发失败阈值）
- **THEN** 请求正常处理，不返回 429

### Requirement: 认证结果缓存

server MAY 对认证结果做进程内缓存以降低每请求数据库开销。缓存 MUST NOT 弱化既有认证语义：token 无效、缺失或已吊销时 MUST仍按 Bearer token 认证要求返回 401，且 MUST NOT 泄露 token 是否存在的信息。缓存内容 MUST 仅限非明文元数据（token 哈希派生键、用户/设备标识与状态枚举、vault 标识与 seq），MUST NOT 存储明文 token 或 vault 口令材料。

缓存失效 SHALL 为双通道：管理员吊销 token 或屏蔽/解封 client 时，server 进程 MUST 经进程间失效广播（PostgreSQL LISTEN/NOTIFY）即时清空认证缓存；缓存条目 TTL MUST NOT 超过 30 秒，仅作为失效广播可能丢失时（如监听连接瞬断）的兜底上限。监听重连时 MUST 清空全部认证缓存。

#### Scenario: 缓存命中不改变认证语义

- **WHEN** 请求携带有效 token 且认证结果命中缓存
- **THEN** 请求按既有语义正常处理，该请求不产生认证 SQL 查询

#### Scenario: 吊销即时生效

- **WHEN** 管理员吊销某 token 后，该 token（认证结果已缓存）访问受保护接口
- **THEN** 失效广播已清空缓存，请求返回 401（吊销命令完成后 ≤1 个请求内）

#### Scenario: 兜底窗口有界

- **WHEN** 失效广播丢失（监听连接瞬断且缓存未及清空）
- **THEN** 已缓存的认证结果至多 30 秒后自动过期，后续请求回库认证

#### Scenario: 缓存内容边界

- **WHEN** 审查缓存驻留内容
- **THEN** 仅有 token 哈希派生键、用户/设备标识与状态枚举、vault 标识与 seq，无明文 token、无 vault 口令材料、无条目密文

