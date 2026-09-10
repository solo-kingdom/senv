## Context

动机见 proposal.md「Why」，行为契约见 `specs/session-auth/spec.md`。现状实现要点：判定集中在 `internal/session/manager.go` 的 `isCacheValid` / `loadValidatedCredential` / `GetCachedKey`；缓存路径解析与单槽命名在 `internal/session/cache.go`（XDG runtime）、`disk_store.go`（逃生舱）、`keychain_store.go`（macOS）；MCP 授权指纹在 `mcp_auth.go`；命令入口在 `cmd/session.go`、`cmd/auth.go`、`cmd/mcp.go`。已归档的 `guard-metadata-data-desync` 建立了 `ErrNoSession` / `ErrSessionExpired` / `ErrSessionStaleMetadata` / `ErrSessionStaleKey` 与 `PeekCachedKey`，本变更沿用并扩展该分类。

## Goals / Non-Goals

**Goals:**

- 把「到期 / 失效 / 不可判定」编码进类型化错误与缓存状态，让清理决策与提示都基于真实原因。
- 让缓存身份跟随 vault（规范化 data path hash），并支持每个 vault 一份槽位与旧单槽收养。
- 提供续期（滑动 + `max_lifetime`）与 `senv session refresh`，同时保持"口令认证不落盘"的默认边界。
- 让 MCP 授权只依赖 vault + 派生密钥，不再受"最近一次 start"影响。

**Non-Goals:**

- 不改 KDF、密文算法、metadata 格式；不改平台安全存储的介质与权限约束。
- 不做跨登录留存、不引入 secret-service、不常规化 `--insecure-cache`。
- 不自动修复 metadata/data desync（既有 `doctor` 路径不变）。

## Decisions

### D1 — 在 `internal/session` 增加失效原因类型

新增哨兵 `ErrSessionInvalidated`、`ErrSessionUnverifiable`，保留 `ErrSessionExpired` 与两条 stale 错误。`isCacheValid` 改为返回 `(valid bool, reason InvalidReason, err error)` 形态的原因枚举（`Expired` / `Invalidated` / `Unverifiable` / `UnknownTimeout`），`loadValidatedCredential` 据此映射哨兵错误。**只有 Expired / Invalidated 触发 `clearCache`**；Unverifiable 一律保留缓存并返回可操作错误。

- 备选：继续用单一 `ErrSessionExpired` + 错误字符串区分。否决——字符串匹配不可靠，且 cmd 层无法据此决定"是否清缓存"。

### D2 — vault 身份与槽位命名

`normalizeVaultPath` = `filepath.Abs` → `Clean` → 对已存在的最长前缀 `EvalSymlinks`（路径尚不存在时退化为父目录解析），再 `hashDataPath`。槽位标识即该 hash（16 hex）。

- Linux runtime：`$XDG_RUNTIME_DIR/senv/session-<uid>-<hash>`；fallback 目录内同名文件。
- 逃生舱：`~/.cache/senv/session-<hash>.json`。
- macOS Keychain：service 仍为 `senv.session.<uid>`，account 改为 `senv.v1.<hash>`；`ClearAll` 循环 `delete-generic-password -s <service>` 直到 item-not-found。
- 旧格式（runtime `session-<uid>`、逃生舱 `session.json`、keychain account `senv.v1`）在首次读取时按 `dataPathHash` 匹配则收养写入新槽；不匹配则保留并在 runtime 目录写一次 `legacy-cache-notice` 标记，提示 `senv session clear --all`。

- 备选：单 keychain item 内存 JSON map。否决——跨进程读改写会丢槽位，且 `--all` 仍要枚举。
- 备选：用 metadata 派生身份做槽位。否决（本轮）——需要缓存格式迁移；`normalizeVaultPath` 已覆盖等价写法问题。

### D3 — 续期语义与存储字段

`SessionCache` 增加可选字段 `timeout_seconds`（duration 的原始 timeout）。续期 `expires_at = min(now+timeout, created_at + max(max_lifetime, timeout))`；`max_lifetime` 来自 settings（默认 24h）。缺失 `timeout_seconds` 的旧 cache 视为不可续期（保留原绝对到期），不报错、不重新认证。只有 `duration` 且实际复用 key 的业务命令续期；`session status` / `doctor` 走只读校验路径不写盘。

`Manager.RenewSession(timeout *SessionTimeout)` 复用 `StartSession` 的 vault mutation lease 与原子写路径；`cmd` 层提供 `senv session refresh`（无 `--timeout` 时沿用缓存 timeout）。`session start` 在会话仍有效时调用同一路径续期、不提示密码；只有口令认证成功才重置 `created_at` 与 `timeout_seconds`。

自动重建（到期/失效后交互式命令落盘新会话）由 settings `session.auto_start`（默认 false）控制，默认仍只做临时认证。

- 备选：把 timeout 编码进 `session_id` 或从 `ExpiresAt-CreatedAt` 反推。否决——续期后反推失真，且污染标识。

### D4 — MCP 授权指纹收敛

`MCPAuthorization` 只保留 `keyHash` / `saltHash` / `dataPathHash`（sessionID 仅留作审计展示）。`matches` 不再比较 sessionID / expiresAt / bootID。撤销消息改为不依赖"重启 MCP"的措辞，且仍在调用业务 manager 前拒绝。

- 备选：保留 sessionID 但允许同 key 接管。否决——多一层实例状态，语义等价却更绕。

### D5 — 原因可见与审计

`Manager` 增加只读的 `DescribeCache()`（供 `session status`）返回状态枚举 + 原因 + 剩余时间 + 是否保留缓存。新增审计事件 `session_invalidated` / `session_unverifiable`，并让此前从未写入的 `session_expire` 在到期清理时落盘；字段只含 session_id、timeout 类型、原因文本。

### 数据流

```
命令入口 (env/text/config/tui/interactive/mcp)
  └─ resolveAuth
      └─ Manager.GetCachedKey
          ├─ normalizeVaultPath → slot=hash(dataPath)
          ├─ loadSlot(slot)  ── 旧单槽 hash 匹配? → 收养写新槽
          │                   └ 同槽多份(平台存储+逃生舱) → Unverifiable(保留)
          ├─ validate
          │   ├─ boot 读取失败                 → Unverifiable(保留)
          │   ├─ boot 不符(restart/never)      → Invalidated (清理+审计)
          │   ├─ expires 已过(duration)        → Expired     (清理+审计)
          │   ├─ salt 不符                     → StaleMetadata(保留, 诊断)
          │   └─ VerifyKey 失败                → StaleKey     (保留, 诊断)
          └─ 成功 → duration 且业务命令? → RenewSession(原子写) → 返回 key
```

### 错误处理策略

| 原因 | 清缓存 | 交互式命令 | 非交互/MCP | status 展示 |
|------|--------|-----------|------------|-------------|
| 到期 | 是 | 提示密码（或 opt-in 自动重建） | 报 `ErrNeedSession` 指引 | 到期 + 建议 refresh/start |
| 失效 | 是 | 提示密码并说明原因（重启/vault 变化） | 报 `ErrNeedSession` | 失效 + 原因 |
| 不可判定 | 否 | 报可操作错误（原因 + 下一步），不静默回退密码 | 同左，不降级为免密 | 不可判定 + 原因 + 缓存已保留 |
| stale(metadata/key) | 否 | 走既有 desync 诊断 | 同左 | 同既有诊断 |

### 存储格式与向后兼容

- 新增可选 JSON 字段 `timeout_seconds`：旧 cache 缺失时降级为"不可续期"，不影响复用。
- 槽位改名不迁移旧文件，采用**读取时收养**；不匹配的旧文件保留不参与判定，避免升级期"多缓存 → 重新认证"。
- macOS keychain 旧 account 只在收养时读取，成功收养后删除旧 account 条目。
- 回滚安全性：旧版本不认识新槽位名，会视为无会话并要求重新认证，但不会删除新文件；无数据损坏路径。

### CLI 使用示例

```bash
senv session start --timeout 8h   # 首次需口令；已有有效会话则免口令续期
senv session refresh              # 免口令延长当前 vault 会话
senv session status               # 显示 Active/到期/失效/不可判定 + 原因 + 是否保留缓存
senv session clear                # 只清当前 vault 槽位
senv session clear --all          # 清全部槽位与旧单槽残留
```

## Risks / Trade-offs

- 滑动续期增加写盘频率与并发写风险 → 复用既有 vault mutation lease + 原子写；只对 `duration` 生效。
- `clear` 默认范围收窄可能被误解为"全清" → `status`/帮助文本显式标注作用域，`--all` 在输出中回显清除范围。
- Keychain 条目数随 vault 增加 → 单条仍为派生密钥 + 元数据，量级不变；`--all` 循环删除已覆盖清理。
- 不可判定期间会话长期不可用 → 错误文本必须给出原因与恢复动作（如解锁 keychain、修复 `/proc`）。
- 旧槽不匹配时保留文件会产生残留 → 一次提示 + `--all` 显式清理；不做静默删除以免破坏"可能是恢复钥匙"的缓存。
- 失去"新 session 即吊销 MCP"的隐含通道 → 主动吊销统一走 `session clear` / 屏蔽，需在 `senv-cli` skill 与 `SESSION_USAGE.md` 写清。

## Migration Plan

1. 发布即生效：无需迁移命令；首次读取自动收养匹配的旧槽。
2. 回滚：旧二进制忽略新槽位（表现为需要重新认证），不删除文件、不损坏数据。
3. 观察点：`session_unverifiable` 审计事件频率（判断环境故障是否常见），以及 `legacy-cache-notice` 是否出现。

## Open Questions

- `session refresh` 是否需要 `--timeout` 覆盖（改 timeout 与改上限语义的组合）——可在实现中按需加，不影响 spec 与任务拆分。
- `max_lifetime` 的默认 24h 是否需要按 timeout 量级分档提示——先按单一可配置值实现。
