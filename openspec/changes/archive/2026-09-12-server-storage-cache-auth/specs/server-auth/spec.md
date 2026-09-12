## ADDED Requirements

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
