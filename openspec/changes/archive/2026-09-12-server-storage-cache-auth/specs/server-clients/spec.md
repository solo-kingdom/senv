## MODIFIED Requirements

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
