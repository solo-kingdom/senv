# 0009-session-expiry-model

持久会话的失效判定分三层，三者互不代偿：**到期（Expired）**——有效时间点已过；**失效（Invalidated）**——成立前提不再满足（系统已重启、所绑定 vault 已变）；**不可判定（Unverifiable）**——环境故障（boot ID 不可读、平台安全存储暂不可用）导致无法确证。只有到期与失效允许清缓存并回退重新认证；不可判定必须保留缓存、报可操作错误，且**不得**降级为免密复用未经验证的派生密钥。与之配套：`duration` 类持久会话只按 `expires_at` 判到期，系统重启不再作废它（只有 `restart` 类以 boot ID 判失效）；绑定用的 data path 在哈希前先绝对化、`Clean` 并解析符号链接，使同一 vault 的等价写法共用一份缓存；`duration` 会话在使用时滑动续期，但保留绝对上限，避免短期口令变成事实上的长期凭据。上限由 `max_lifetime` 配置（默认 24h），有效上限取 `max(max_lifetime, timeout)`，续期把 `expires_at` 推到 `min(now+timeout, created_at+max_lifetime)`；只有实际复用缓存的业务命令触发续期，`session status` / `doctor` 这类只读命令不续期，只有显式重新认证（`session start` / `refresh`）才重置 `created_at`。在仍有效的会话上执行 `session start` 直接用缓存 key 续期，不再要求口令；跨进程自动重建持久会话只作为 opt-in 配置、默认关闭。

理由是现状把三层判定折叠成了 `ErrSessionExpired` 一条路径：`internal/session/manager.go` 的 `isCacheValid` 把「读不到 boot ID」这类瞬时故障与「时间已到」等同，`GetCachedKey` 随即 `clearCache()`。结果是环境抖动就能销毁唯一能解开用户数据的派生密钥、并强制重新认证；同一 vault 只因为 `--path` 换了个写法就被判成「没有会话」；重启还会提前杀掉 8h 会话。把安全判定（绑定是否仍成立）与可用性判定（是否值得让用户重输口令）解耦，是这一模型要修掉的根本问题。

## Considered Options

- **不可判定时 fail-open（读不到 boot ID 就继续复用）**：重新认证最少，但等于取消重启失效保证。否决。
- **维持现状（任何判定失败 → 清缓存 → 回退口令）**：重新认证最多，且瞬时故障会销毁恢复用密钥。否决。
- **`duration` 也按 boot ID 判失效**：与「8h 内免认证」的用户预期冲突；密钥本就在内存文件系统/系统钥匙串里，重启并不额外缩小泄露面。否决。
- **data path 用 metadata 派生身份而非路径哈希**：语义更准，但涉及缓存格式与迁移；本轮先做规范化，身份变更留给独立变更。

## Consequences

- `ErrSessionExpired` 不再是「缓存不可复用」的总称；cmd 层与 `session status` 必须区分三层并给出可操作信息（原因 + 下一步），`session_expire` / 失效事件进审计（现 `AuditSessionExpire` 定义了但无写入点）。
- 逃生舱残留（`~/.cache/senv/session.json` 与平台存储并存）等多缓存冲突必须报「请 `senv session clear`」这类可操作错误，不能按到期静默处理；该状态当前每次都复现，用户无从判断。
- 不可判定期间，非交互调用（MCP、`env export`）仍会失败，但缓存不被破坏；环境恢复后免认证复用，不产生额外口令交互。
- 滑动续期需要写缓存，沿用既有原子写与 flock 路径；绝对上限必须存在，否则等于把短期口令制度化。
- 不引入凭据代理或新存储后端；本决策只改判定与清理语义。

## Status

accepted，决策已收敛。实现按 A/B/D（判定与清理正确性、路径规范化、原因可见）与 C（滑动续期、跨登录留存）拆分；C 的实现另开变更。跨登录留存决定维持 tmpfs（`XDG_RUNTIME_DIR`）默认、记 backlog，不把 `--insecure-cache` 常规化。
