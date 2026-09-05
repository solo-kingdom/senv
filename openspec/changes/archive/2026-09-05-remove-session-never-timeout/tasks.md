## 1. 解析层移除 never（核心实现 + 配对测试）

- [x] 1.1 `internal/session/timeout.go`：删除 `never/infinite/forever` 解析分支，错误信息列出支持取值（`supported: 30m, 8h, 1d, 1y, restart`）；删除 `String()` 的 `TimeoutNever` 分支。验证：`go build ./...`
- [x] 1.2 `internal/session/timeout_test.go`：删除 `{"never", ...}` 正向用例与 `TimeoutNever` 的 String 用例，新增 `never`/`infinite`/`forever` 返回 error 的负向用例，断言错误信息含 `restart`。验证：`go test ./internal/session/ -run TestParseTimeout -v`

## 2. 会话层清理（核心实现 + 配对测试）

- [x] 2.1 `internal/session/types.go`：删除 `TimeoutNever` 常量，更新 `TimeoutType` JSON 注释为 `"duration", "restart"`。验证：`go build ./...`
- [x] 2.2 `internal/session/manager.go`：`isCacheValid` 合并分支改为仅 `TimeoutRestart`；确认遗留 `"never"` cache 落入 `default` 分支报 `unknown timeout type`。验证：`go build ./...`
- [x] 2.3 `internal/session/manager_test.go`：所有 `ParseTimeout("never")`/`TimeoutNever` fixture 改为 `restart`，断言不变；新增用例：手工构造 `timeout_type: "never"` 的 cache，断言 `IsCacheValid` 返回无效且 `session status` 语义为过期（不崩溃）。验证：`go test ./internal/session/ -race`
- [x] 2.4 `internal/session/cache_test.go`、`disk_store_test.go`、`kdf_boundary_test.go`、`fallback_lock_test.go`：`never` fixture 改为 `restart`。验证：`go test ./internal/session/ -race`

## 3. cmd 层文案与测试（高优先级：安全边界用例）

- [x] 3.1 `cmd/session.go`：Long 帮助文本移除 never 选项说明；删除 `TimeoutNever` 的成功提示与 status 输出分支；错误路径确认含可用值提示。验证：`go run . session start --help` 与 `go run . session start -t never`（预期报错）
- [x] 3.2 `cmd/init.go`、`cmd/env.go`：提示文案 `session start -t never` 改为 `-t restart`。验证：`rg -n "never" cmd/ 仅剩测试负向用例`
- [x] 3.3 `cmd/auth_test.go`、`desync_test.go`、`mcp_guard_test.go`、`mcp_guard_all_tools_test.go`：`never` fixture 改 `restart`；`mcp_guard_test.go` 新增 `never` 被拒的负向用例。验证：`go test ./cmd/ -race`
- [x] 3.4 `cmd/security_boundary_cli_test.go`：`--timeout never` 用例改为断言命令失败且错误含 `restart`。验证：`go test ./cmd/ -run SecurityBoundary -race`
- [x] 3.5 `internal/storage/sensitive_boundaries_test.go`：`settings.Session.Timeout = "never"` 用例改为断言 start 报错；`internal/storage/types.go` 注释移除 `never`。验证：`go test ./internal/storage/ -race`

## 4. 文档同步

- [x] 4.1 `SESSION_USAGE.md`：超时格式表与示例移除 `never`，安全建议表移除对应行，FAQ 与场景示例改 `restart`。验证：`rg -n "never" SESSION_USAGE.md` 仅剩「已移除」说明（如有）
- [x] 4.2 `README.md`、`docs/SECURITY.md`：移除 `never` 引用；SECURITY.md 的「`never` 仅表示不设时间过期」表述改为所有模式统一语义。验证：`rg -n "never" README.md docs/SECURITY.md`

## 5. 全量验证

- [x] 5.1 `rg -n "TimeoutNever|\"never\"" --glob '*.go'` 确认仅负向测试用例残留。验证：输出符合预期清单
- [x] 5.2 `make check`（fmt + vet + lint + `go test -race ./...`）全绿。验证：命令退出码 0
