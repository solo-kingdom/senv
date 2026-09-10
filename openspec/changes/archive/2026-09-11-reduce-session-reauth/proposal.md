## Why

持久会话把「到期 / 绑定失效 / 环境故障（boot ID 读不到、平台存储暂不可用）」折叠成同一条 `ErrSessionExpired`，于是环境抖动就 `clearCache()` 销毁派生密钥并强制重新认证；同一 vault 换 `--path` 写法就被判「无会话」；任何一次 `session start` 还吊销运行中的 MCP。这些重新认证都不是安全需要。

## What Changes

- 判定分三层（到期 / 失效 / 不可判定）；只有前两者清缓存，不可判定保留缓存并报可操作错误，不 fail-open。
- `duration` 只按时限到期、不受重启影响，只有 `restart` 看 boot ID；遗留 `never` 按 `restart` 收养。
- data path 哈希前规范化；缓存按 vault 分槽；旧单槽按 hash 收养；`session clear` 默认清当前 vault、`--all` 清全部。
- 新增 `senv session refresh`；`duration` 使用即续期，上限由 `max_lifetime`（默认 24h，可配置）与显式 timeout 约束；有效会话上 `session start` 免口令续期；自动重建仅 opt-in。
- MCP 授权指纹只绑 `keyHash + saltHash + dataPathHash`。**BREAKING**：反转既有「session 被替换即拒绝」语义，主动吊销统一走 `clear` / 屏蔽。
- `session status` 区分三层原因与缓存是否保留；补 `session_expire` / 失效 / 不可判定审计（现无写入点）。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `session-auth`: 失效判定分层与非破坏性处理、`duration` 生命周期、缓存绑定与分槽、续期与 `session refresh`、MCP 授权绑定、原因可见与审计。

## Non-goals

- 不做跨登录留存：维持 tmpfs 默认，不常规化 `--insecure-cache`。
- 不改加密算法、KDF、metadata 格式与 cache 结构；不用 metadata 派生身份做槽位；不新增凭据代理。

## Security Analysis

- 保留缓存不等于放行：都必须再经过 metadata salt 匹配与 `VerifyKey`，未验证的 key 不复用。
- 滑动续期是唯一扩大 key 驻留面的改动，以 `max_lifetime` 绝对上限兜底；只读命令（`session status` / `doctor`）不续期；免口令续期只对已验证有效的缓存生效。
- 分槽不改变单槽的加固约束与泄露面；`clear --all` 保留彻底清理路径。
- 状态与审计新增字段 MUST NOT 泄露 key、salt 或任何可离线爆破的派生材料。

## Impact

- `internal/session/`：判定分层与清理、续期、分槽与旧文件收养、MCP 指纹、审计事件。
- `cmd/`：`session refresh`、`--all`、status 原因展示、提示语与 opt-in 自动重建。
- 文档/技能同步：`.agents/skills/senv-cli/SKILL.md`、`SESSION_USAGE.md`、`README.md`；决策见 ADR-0009/0010。
