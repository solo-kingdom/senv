# server-clients Specification

## Purpose
定义 senv-server 的 client 设备身份管理：client 凭一次性注册码注册获得专属凭证，管理员按设备屏蔽/解封；被屏蔽 client 的请求被明确拒绝，client 端感知后清理本机解锁缓存而保留加密数据。
## Requirements
### Requirement: 一次性注册码签发
系统 SHALL 提供管理员 CLI 为指定用户签发一次性注册码，注册码 SHALL 带过期时间且仅存哈希；注册成功或过期后注册码 SHALL 立即失效。

#### Scenario: 签发注册码
- **WHEN** 管理员为某用户执行注册码签发命令
- **THEN** 输出一次性注册码与过期时间，库中仅保存其哈希

#### Scenario: 注册码过期
- **WHEN** 使用已过期的注册码注册
- **THEN** 注册被拒绝，该注册码不可再次使用

### Requirement: client 注册换取专属凭证
client SHALL 能凭有效注册码、设备名与 server 地址完成注册，获得绑定到该 client 记录的专属 token；明文 token 仅在注册响应中展示一次，server 仅存哈希。注册端点 SHALL 受与认证失败相同的限速保护。

#### Scenario: 注册成功
- **WHEN** client 使用有效注册码与同用户下未占用的设备名注册
- **THEN** server 创建 client 记录并返回一次性明文 token，client 将其存入本机设置

#### Scenario: 设备名冲突
- **WHEN** 同一用户下已存在同名 client
- **THEN** 注册失败并明确提示，不产生重复记录

#### Scenario: 注册码重放
- **WHEN** 已使用过的注册码再次用于注册
- **THEN** 注册被拒绝

### Requirement: 屏蔽与解封

系统 SHALL 提供按 client 屏蔽与解封的管理命令。屏蔽 SHALL 使该 client 名下所有凭证立即失效：屏蔽命令完成后，该 client 凭证的后续请求 MUST 被拒绝（403）。server 存在认证结果缓存时，屏蔽与解封命令 MUST 触发失效广播使缓存即时失效；广播丢失时缓存兜底 TTL SHALL 保证至多 30 秒内生效。解封后该 client 既有凭证经同一失效广播恢复有效，至多延迟一个兜底窗口。屏蔽与解封 MUST NOT 影响 server 侧该用户的数据，MUST NOT 影响同用户其他 client。

#### Scenario: 屏蔽后请求被拒

- **WHEN** 已屏蔽 client 持其凭证访问任意 API
- **THEN** 返回 403 并携带机器可读的屏蔽标识，与 401 可区分

#### Scenario: 屏蔽穿透缓存

- **WHEN** 某 client 的认证结果已在缓存中，管理员随后屏蔽该 client
- **THEN** 该 client 凭证的下一个请求即被拒绝（403），不依赖兜底 TTL 过期

#### Scenario: 解封恢复

- **WHEN** 管理解封某 client 后其持原凭证访问
- **THEN** 请求恢复正常处理（经失效广播即时生效；广播丢失的兜底窗口内可能短暂 403）

#### Scenario: 数据不受影响

- **WHEN** 某 client 被屏蔽
- **THEN** 该用户 vault 及其他 client 的访问不受影响

### Requirement: 屏蔽响应不泄露额外信息
被屏蔽响应 MUST NOT 包含用户、vault 或数据存在性信息；对 server 未知的 token 仍按 401 处理，MUST NOT 泄露屏蔽语义。

#### Scenario: 未知 token 不泄露屏蔽语义
- **WHEN** 携带 server 未知的 token 访问
- **THEN** 返回 401，响应与该 token 从未存在时一致

### Requirement: client 感知屏蔽并清理本地状态
client 在任一 server 请求收到屏蔽标识时 SHALL：清空本地解锁缓存、保留本地加密工作副本、给出明确屏蔽提示与重新注册指引，并以非零码退出；屏蔽解除前后续命令 SHALL 重复该行为。

#### Scenario: 自动清理解锁缓存
- **WHEN** 已被屏蔽的 client 执行任意需访问 server 的命令
- **THEN** 本地解锁缓存被清除，输出屏蔽提示与重新注册指引，命令以非零码退出

#### Scenario: 本地数据不被删除
- **WHEN** client 检测到被屏蔽
- **THEN** 本地加密工作副本不被删除或改写

