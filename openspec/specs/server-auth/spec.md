# server-auth Specification

## Purpose
定义 senv-server 的多用户身份与访问控制：用户持有 Bearer token 访问自己的 vault，server 只存 token 哈希，vault 加密口令永不经过 server。
## Requirements
### Requirement: Bearer token 认证

除健康检查与用户创建外的所有 API SHALL 要求 `Authorization: Bearer <token>`。token 无效、缺失或已吊销时 MUST 返回 401，MUST NOT 泄露 token 是否存在的信息。server MUST 只存储 token 的哈希，MUST NOT 存储或可反推明文 token。

#### Scenario: 无 token 访问

- **WHEN** 请求受保护接口且未携带 token
- **THEN** 返回 401，响应体不含任何用户/vault 信息

#### Scenario: 吊销后失效

- **WHEN** 某 token 被吊销后用其访问接口
- **THEN** 返回 401，与该 token 从未存在过的响应一致

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
