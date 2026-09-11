## 1. 清理边界收紧（高优先级 · 安全）

- [x] 1.1 `internal/session/manager.go`：`GetCachedKey` 只在 `ReasonExpired` 时 `clearCache`；`ReasonRestarted` / `ReasonVaultChanged` 保留缓存并返回带 reason 的 `ErrSessionInvalidated`。验证：新增/改 `internal/session/manager_test.go` 断言失效缓存调用后文件仍在。
- [x] 1.2 与 1.1 配对测试：构造 restart boot ID 变化与 vault 不匹配两种失效，断言 `GetCachedKey` 返回失效错误、缓存未删、审计写入 `session_invalidated`。验证：`go test ./internal/session -run TestGetCachedKeyPreservesOnInvalidated`。

## 2. 多缓存确定选择（高优先级）

- [x] 2.1 `internal/session/cache.go`：`loadCache` 改为 `created_at` 新者优先，返回被忽略项；`created_at` 相同时才返回可操作错误。验证：新增单测覆盖"新者优先""同刻报错""两份都保留"。
- [x] 2.2 与 2.1 配对测试：断言被选中的缓存仍走完整 salt/key 校验，伪造的更新缓存不能绕过。验证：`go test ./internal/session -run TestLoadCacheMultipleSelection`。
- [x] 2.3 移除或保留 `errMultipleSessionCaches` 的引用并更新 `internal/session/store.go` 注释；确认无死代码。验证：`go build ./...` + `go vet ./...`。

## 3. 重认证根因结构（高优先级）

- [x] 3.1 `internal/session/types.go` 新增 `AuthRootCause` 枚举（`expired`/`restarted`/`vault-changed`/`multiple-cache`/`unreadable`/`metadata-replaced`），并在错误包装处填充。验证：`go build ./...`。
- [x] 3.2 `cmd/auth.go`：`resolveAuth` 出口按 `AuthRootCause` 渲染「原因 + 一条确定动作」；非交互与 MCP 仍折叠为 `ErrNeedSession`。验证：`go test ./cmd -run TestResolveAuthRootCauseMessages`。
- [x] 3.3 与 3.2 配对测试：每个根因断言恰好给出一条下一步动作，且不泄露 key/salt/口令。验证：`go test ./cmd -run TestAuthErrorNoSecretLeak`。

## 4. `session status` 与 `session refresh` 输出

- [x] 4.1 `cmd/session.go`：Active 态增加「距绝对上限剩余时间」行；`Invalidated` / `Unverifiable` 态明确显示「Cache: retained / Next: <单条动作>」。验证：新增 `cmd/session_status_test.go` 断言输出。
- [x] 4.2 `cmd/session.go`：`refresh` 按 `AuthRootCause` 分支渲染错误，保留"不新建、不删缓存、不弹口令"语义。验证：`go test ./cmd -run TestSessionRefreshErrorMessages`。
- [x] 4.3 与 4.2 配对测试：断言 `refresh` 在 Expired / Invalidated / Unverifiable 下均不修改缓存文件、不触碰 stdin。验证：`go test ./cmd -run TestSessionRefreshNoMutation`。

## 5. 文档与 agent 指引同步

- [x] 5.1 更新 `SESSION_USAGE.md`：状态四态、多缓存选择、根因与下一步动作。验证：通读与 `session status` 实际输出一致。
- [x] 5.2 更新 `.agents/skills/senv-cli/SKILL.md` 会话小节：失效缓存默认保留、多缓存不再卡死、错误给单条动作。验证：`go run . session status --help` 与 skill 描述一致。
- [x] 5.3 新增 ADR `docs/adr/0017-session-reauth-root-cause.md`：记录"自动清理仅限到期""多缓存新者优先"两项决策。验证：文件存在且 Status/Consequences 完整。

## 6. 回归与验证

- [x] 6.1 全量测试：`make test`（`go test -race ./...`）通过。验证：命令退出码为 0。
- [x] 6.2 端到端手测：建立会话 → 模拟 boot ID 变化 → 确认缓存保留且提示单条动作 → `senv session refresh` 不弹口令。验证：手工记录输出与 expectations 一致。
- [x] 6.3 `make check`（fmt + vet + lint + test）通过。验证：命令退出码为 0。
