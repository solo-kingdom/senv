## 1. 失效原因类型化（核心）

- [x] 1.1 【安全】在 `internal/session` 增加 `InvalidReason`（`Expired`/`Invalidated`/`Unverifiable`）与哨兵 `ErrSessionInvalidated`、`ErrSessionUnverifiable`，把 `isCacheValid` 拆成返回原因的校验，`loadValidatedCredential` 按原因映射错误。验证：`go build ./internal/session` 且 `go test ./internal/session -run 'TestErrorClass|TestLegacyNever'`。
- [x] 1.2 【安全】`GetCachedKey` 改为只在 `Expired`/`Invalidated` 清理缓存；`Unverifiable` 保留缓存并返回带原因的可操作错误。验证：新增单测覆盖 boot ID 读取失败时缓存文件仍存在且错误可被 `errors.Is` 识别。
- [x] 1.3 遗留 `timeout_type: "never"` 按 `restart` 收养（含 boot ID 判定）。验证：单测断言 never cache 在 boot ID 未变时复用、变化时判 Invalidated。

## 2. vault 身份、槽位与旧缓存收养

- [x] 2.1 实现 `normalizeVaultPath`（Abs + Clean + 已存在最长前缀 `EvalSymlinks`）并让 `hashDataPath` 基于规范化结果；槽位命名接入 runtime/fallback/逃生舱三处。验证：`go test ./internal/session -run TestNormalizeVaultPath`，覆盖尾斜杠、相对路径、符号链接、非存在路径。
- [x] 2.2 macOS `keychainStore` 改 per-slot account（`senv.v1.<hash>`），实现按槽读/写/删与 `ClearAll`（循环删除）。验证：`go test ./internal/session -run TestKeychainStore`（fake runner 断言参数与循环终止）。
- [x] 2.3 旧单槽（runtime `session-<uid>`、逃生舱 `session.json`、keychain `senv.v1`）按 hash 匹配收养；不匹配则保留并在 runtime 写一次 `legacy-cache-notice`。验证：单测覆盖匹配收养、不匹配保留、提示只出现一次（`TestLegacyNoticeIsShownOnce`）。
- [x] 2.4 同槽多份缓存（平台存储 + 逃生舱）判 `Unverifiable` 并提示 `senv session clear --all`，不得静默清理。验证：单测断言两份缓存共存时返回 Unverifiable 且两文件都在。

## 3. 续期与 `session refresh`

- [x] 3.1 【安全】`SessionCache` 增加可选 `timeout_seconds`；实现 `Manager.RenewSession`，按 `min(now+timeout, created_at+max(max_lifetime, timeout))` 更新 `expires_at`，复用 vault mutation lease 与原子写。验证：单测覆盖续期上限、旧 cache 缺字段时不可续期。
- [x] 3.2 settings 增加 `session.max_lifetime`（默认 24h）与 `session.auto_start`（默认 false），并接入 `RenewSession` 与交互式重建判定。验证：`go test ./internal/storage -run TestNewSettings` 与 `go test ./cmd -run TestResolveAuthAutoStartOptInPersistsSession`。
- [x] 3.3 业务命令复用 key 时触发 `duration` 续期；`session status`/`doctor` 不触发。验证：单测断言两次读之间的 `expires_at` 变化（业务路径）与不变（只读路径）。
- [x] 3.4 新增 `senv session refresh`（无口令；到期/失效/不可判定时报原因与下一步），`session start` 在有效会话上免口令续期，`session clear` 默认当前 vault、新增 `--all`。验证：`go test ./cmd -run 'TestSession(Refresh|Clear)'` 与 `go run . session refresh --help`。
- [x] 3.5 自动重建仅 opt-in：默认关闭时临时密码仍不落盘。验证：单测断言 `auto_start=false` 下命令成功后 `session status` 仍无 active session。

## 4. MCP 授权绑定到 vault

- [x] 4.1 【安全】`MCPAuthorization` 收敛为 `keyHash + saltHash + dataPathHash`（sessionID 仅审计用），更新撤销消息与失败拒绝路径。验证：`go test ./internal/session -run TestAuthorizeMCPRequest`。
- [x] 4.2 集成测试：同一 vault 重新 `session start`（新 session ID）后 MCP 请求继续成功；`session clear` 与 salt 变化仍拒绝。验证：`go test ./cmd -run TestMCPRequestSessionGuard`。

## 5. 原因可见与审计

- [x] 5.1 `Manager` 增加只读状态描述（状态 + 原因 + 剩余时间 + 是否保留缓存），`senv session status` 与交互式会话菜单据此展示。验证：`go test ./cmd -run TestSessionStatusReportsFourStates` 覆盖 Active/到期/失效/不可判定四态。
- [x] 5.2 【安全】补齐审计事件 `session_expire` / `session_invalidated` / `session_unverifiable`（只含 session_id、timeout 类型、原因文本）。验证：`go test ./internal/session -run TestAuditFailureEventsAreSanitized`，断言事件落盘且不含 key/salt/明文。

## 6. 文档与 agent 指南同步

- [x] 6.1 更新 `.agents/skills/senv-cli/SKILL.md`：`session refresh`、`clear --all`、状态四态、MCP 不再因重新 start 失效。验证：`go run . --help && go run . session --help && go run . session refresh --help` 与 skill 描述一致。
- [x] 6.2 更新 `SESSION_USAGE.md` 与 `README.md`：续期与上限、分槽与清理解释、示例命令。验证：文档中的命令逐条实跑通过（临时 `HOME`/`XDG_RUNTIME_DIR` 下）。
- [x] 6.3 在 `docs/RELEASE_NOTES.md` 记录本次行为变更（含 MCP 语义反转与 `duration` 不再受重启影响）。验证：`git diff --stat` 覆盖上述文件且措辞与 ADR-0009/0010 一致。

## 7. 回归与收尾

- [x] 7.1 全量回归：`make check`（fmt + vet + lint + `go test -race ./...`）。验证：命令退出码 0；失败项全部属于本次改动范围。
- [x] 7.2 手工冒烟：在临时 `HOME` 与 `XDG_RUNTIME_DIR` 下走完 start → status → refresh → 等价路径复用 → clear / clear --all → 不可判定（损坏缓存；boot ID 不可读路径由 `TestUnverifiableBootIDKeepsCache` 覆盖），落成可复现用例 `cmd/session_smoke_test.go:TestSessionLifecycleSmoke`。验证：每步输出与 spec 场景一致，`audit.log` 出现 `session_start` / `session_clear` / `session_unverifiable` 且不含 key。
- [x] 7.3 按 `openspec validate reduce-session-reauth` 与 `openspec status --change reduce-session-reauth` 确认实现完成后再归档（`openspec archive`）。验证：validate 无 error，checkbox 全部勾选。
