# 0010-vault-bound-session-identity

持久会话的身份绑定在 vault 上，而不绑定在某一次会话实例上。三条落地：缓存按规范化后的 data path hash 分槽，每个 vault 一份、互不覆写；MCP server 的授权指纹只由 `keyHash + saltHash + dataPathHash` 构成，不再包含 `sessionID` / `expiresAt` / `bootID`（`internal/session/mcp_auth.go`）；`senv session clear` 默认只清当前 vault 的槽，`--all` 才清全部。旧单槽缓存（`session-<uid>` 与 `~/.cache/senv/session.json`）在首次读取时按 `dataPathHash` 匹配则收养为对应 vault 槽位，不匹配则原样保留并提示一次 `senv session clear --all`——不静默删除可能是恢复钥匙的缓存，也不让升级路径复现"多缓存 → 每次重新认证"。

理由是现状的两种"重新认证"都不是安全需要：per-uid 单槽使切换 vault 覆盖掉上一个 vault 的会话；指纹含会话实例字段使任何一次 `senv session start` 都让运行中的 MCP server 报 "session expired or revoked"，只能重启 agent 的 MCP process。会话是否仍可用，应由每次请求的实时校验（到期、绑定前提、密钥与盐）判定，不需要"最近一次 start 的实例标识"这种代理信号。

## Considered Options

- **维持全局单槽**：换 vault 即重新认证，多 vault 用户永远互相踩。否决。
- **MCP 保留 sessionID、允许同 key 新会话自动接管**：行为等价但多留一层实例状态，语义更绕。否决。
- **用 metadata 派生身份做槽位**：语义更准，但需要缓存格式迁移；留作后续变更候选。

## Consequences

- 多 vault 不再互相覆写；`senv session status` 要按当前 data path 解释"本 vault 的会话"。
- MCP server 只在缓存被清、到期或密钥/盐变化时失效；"重新 `session start` 后 MCP 仍可用"成为预期行为，需同步 `senv-cli` skill 与 `SESSION_USAGE.md`。
- 旧单槽缓存的迁移与分槽同批实现；`clear` 的默认范围收窄为当前 vault 后，"怀疑泄露"与升级清理要显式用 `--all`。
- 失去隐含的"新会话即吊销旧 MCP"通道；主动吊销统一走 `senv session clear` 与屏蔽。

## Status

accepted，决策已收敛。实现未开始；滑动续期的绝对上限语义见 [ADR-0009](./0009-session-expiry-model.md)。
