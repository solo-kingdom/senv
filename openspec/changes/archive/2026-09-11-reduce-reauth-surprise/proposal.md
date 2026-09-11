## Why

把"用户重新输入密码"当成严重体验问题来审视后，senv 有一半的重认证触发点并非安全需要：瞬时环境故障（boot ID 抖动）、同槽多份缓存、metadata 被 git pull 替换，都会让用户突然被踢回密码输入，甚至在最坏情况下丢失唯一能解密数据的缓存。ADR-0009/0010 已定下"不可判定必须保留缓存"的原则，但代码里仍有判定与清理路径与之不一致，用户既看不到原因、也拿不到恢复动作。

## What Changes

- **重认证根因可见**：`session status` 与命令错误统一给出"为什么需要密码"的机器可读原因（到期 / 重启 / 换 vault / 多缓存 / 环境不可判定 / metadata 失步），并附一条确定的下一步动作。
- **收紧破坏性清理**：只有 `ReasonExpired` 才允许自动清缓存；`ReasonRestarted` / `ReasonVaultChanged` 改为"保留缓存 + 报可操作错误"，不再静默 `clearCache`。
- **多缓存自愈**：同一槽同时命中平台存储与磁盘逃生舱时，按"新者优先、旧者保留"给出确定选择，而不是一律 `ErrSessionUnverifiable` 把用户挡死。
- **会话续期不推回密码**：`session refresh` 在 `Expired` / `Invalidated` 时给出明确原因与单条修复动作，不再笼统要求 `session start`。
- **绝对上限可调**：`session.max_lifetime` 的默认值与上限语义保持，但 `session status` 明确显示"距绝对上限还剩多久"，避免"用得好好的突然要密码"。

## Non-goals

- 不取消会话到期机制，不引入无限期免密。
- 不新增凭据代理、不引入操作系统钥匙串作为缓存后端（沿用 ADR-0015/0016）。
- 不改变 `passwd` / `init` 的显式口令流程。
- 不改动 server 侧认证与屏蔽语义。

## 安全性分析

- 保留缓存只改变"何时提示密码"，不改变"何时允许解密"：每次复用仍按 data path 绑定、salt、cached key 三重校验，校验不过一律拒绝，绝不放行未经验证的派生密钥。
- 多缓存自愈只做选择与提示，不删除旧缓存；被选中的那份仍需通过完整校验，攻击者预置一份伪造缓存无法绕过。
- 收紧自动清理会保留更多派生物，但缓存本就位于经确认的 memory-backed 文件系统，且新增保留路径全部写入审计（`session_unverifiable` / `session_invalidated`）。

## Capabilities

### New Capabilities
（无）

### Modified Capabilities
- `session-auth`: 重认证原因可见、破坏性清理边界收紧、多缓存确定选择、`session refresh` 错误翻译。

## Impact

- `internal/session/manager.go`（`GetCachedKey` 清理边界、`cacheValidity`）、`internal/session/cache.go`（多缓存选择）、`internal/session/errors.go`、`internal/session/types.go`。
- `cmd/session.go`（`status` / `refresh` 输出）、`cmd/auth.go`（错误翻译）、`cmd/mcp.go`（非交互提示）。
- 文档：`SESSION_USAGE.md`、`.agents/skills/senv-cli/SKILL.md`（错误信息与下一步动作变化）。
- 无存储格式变更，无 CLI 参数破坏性变更。
