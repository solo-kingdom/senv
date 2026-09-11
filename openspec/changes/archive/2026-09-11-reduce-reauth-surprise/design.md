## Context

见 proposal.md — Why。当前实现（`internal/session/manager.go`）在 `GetCachedKey` 里对 `ErrSessionExpired` 与 `ErrSessionInvalidated` 一律 `clearCache`，而 ADR-0009/0010 已要求「失效」与「不可判定」都保留缓存。`loadCache`（`internal/session/cache.go`）在平台存储与磁盘逃生舱同时命中时直接返回 `errMultipleSessionCaches`，把选择责任推给用户。`cmd/session.go` 的 `refresh` 在非 Active 态统一返回 `run: senv session start`，无法区分根因。

约束：缓存位于经确认的 memory-backed 文件系统（ADR-0016），磁盘逃生舱为 0600；无存储格式变更意愿；不引入新依赖。

## Goals / Non-Goals

**Goals:**
- 让「为什么要重新输密码」在命令错误与 `session status` 里都可判定、可执行。
- 让自动删除缓存只发生在唯一安全的情形：`duration` 到期。
- 让多缓存从「卡死」变成「确定选择 + 保留 + 可清理」。

**Non-Goals:**
- 不改缓存 JSON 结构，不迁移既有缓存。
- 不改 vault 槽位哈希算法。
- 不引入交互式"帮我选一份缓存"的提示。

## Decisions

### 1. 清理边界收紧到 `ReasonExpired`

`GetCachedKey` 只在 `cacheValidity` 返回 `ReasonExpired` 时调用 `clearCache`。`ReasonRestarted` / `ReasonVaultChanged` 转为一个新的可保留错误（沿用 `ErrSessionInvalidated`，但携带 reason），不再清缓存。

**替代方案**：让 `Invalidated` 也清理（现状）——否决，因为它会删掉另一 vault 的恢复钥匙。

### 2. 多缓存确定选择：新者优先

`loadCache` 不再返回 `errMultipleSessionCaches`；改为比较两份缓存的 `created_at`，新者作为本次读取结果，旧者保留，并通过一个新的 `ignoredCache` 字段/返回值上报被忽略项。仅当 `created_at` 完全相等时返回可操作错误。

**替代方案**：
- 一律报错（现状）——否决，用户被挡死且无信息判断。
- 静默取平台存储优先——否决，逃生舱往往是用户显式创建的，可能更新。

### 3. 重认证根因的结构化上报

新增 `AuthRootCause`（或等价枚举）随错误返回：`expired` / `restarted` / `vault-changed` / `multiple-cache` / `unreadable` / `metadata-replaced`。`cmd` 层据此渲染「原因 + 一条确定动作」，MCP 路径折叠为既有的 `ErrMCPRevoked` 文案不变。

**替代方案**：继续用错误字符串拼接——否决，`session status` 与命令错误无法保证一致。

### 4. `session refresh` 按状态翻译

`refresh` 保留现有拒绝语义（不新建、不删缓存、不弹口令），但错误信息按 `AuthRootCause` 分支，每支给一条确定动作。

### 5. 数据流

```
命令需要解密
  └─ resolveAuth
       └─ GetCachedKey
            ├─ loadCache（平台存储 + 逃生舱，多份 → 新者优先 / 同刻报错）
            ├─ cacheValidity → Reason{None,Expired,Restarted,VaultChanged,Unreadable,UnknownType}
            ├─ ReasonExpired      → 清缓存 → ErrSessionExpired
            ├─ ReasonRestarted/VaultChanged → 保留 → ErrSessionInvalidated(+reason)
            ├─ ReasonUnreadable   → 保留 → ErrSessionUnverifiable
            └─ ReasonNone → salt/key 校验
                 ├─ stale metadata → ErrSessionStaleMetadata（保留）
                 └─ ok → 返回 key + 续期
       └─ 错误 → 渲染 AuthRootCause → 命令错误 / session status
```

### 6. 错误处理策略

- 归一化：`cmd` 层只依赖 `AuthRootCause`，不再靠 `errors.Is` 串多个 sentinel。
- 安全不变：任何保留路径都不影响"校验不过即拒绝解密"。
- 审计：`Invalidated` / `Unverifiable` 保留时仍写 `session_invalidated` / `session_unverifiable`，新增多缓存选择事件字段（只含槽位标识与选中的时间，不含 key）。
- 非交互/MCP：保持折叠为 `ErrNeedSession` / `ErrMCPRevoked`，文案更新为可执行。

### 7. CLI 使用示例（无参数变更）

```
$ senv session status
Session: Invalidated
Reason: system rebooted (restart session)
Cache: retained
Next: senv session start

$ senv session status
Session: Active
Timeout: duration
Expires: 2026-09-11 20:00:00 (in 3h20m)
Session cap: 2026-09-12 08:00:00 (in 15h20m)
```

## Risks / Trade-offs

- [收紧清理后旧缓存长期堆积] → `session status` 明确显示保留与清理方式；`session clear --all` 仍是显式出口。
- [新者优先可能选了被篡改的缓存] → 选中项仍需完整 salt/key 校验，校验不过即拒绝解密。
- [`created_at` 同刻罕见但存在] → 报可操作错误，不静默选择。
- [根因枚举扩散到 cmd 层] → 只在 `resolveAuth` 出口做一次归一化，命令层不各自解析 sentinel。

## Migration Plan

无存储格式迁移，无参数变更。发布后旧缓存按现有校验被读取；行为差异仅在不安全清理与多缓存报错处。回滚即回到旧二进制，缓存文件始终向后兼容。

## Open Questions

无。
