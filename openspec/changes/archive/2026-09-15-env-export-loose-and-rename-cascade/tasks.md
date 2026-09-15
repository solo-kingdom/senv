## 1. export 宽松解引用（Phase A）

- [x] 1.1 在 `cmd/` 抽出 `resolveExportShell(envMgr, textMgr) (string, []string, error)`：对激活组变量逐条 `ResolveWithWarnings(..., Loose:true)`，warning 前缀 `warning: env export: <KEY>:`，结构性错误向上返回。验证：`go test ./cmd -run Export -count=1`
- [x] 1.2 `cmd/env.go` 的 `envExportCmd` 改用 helper：warnings → stderr（`ref.PrintWarnings` 或包装），成功 exit 0；移除 `failed to resolve references in export` 对缺失目标的 hard fail。验证：手工 `go run . env export` 在一条 stale ref 下仍输出其余行
- [x] 1.3 `cmd/mcp.go` 的 `envExport` 共用同一 helper，MCP 失败语义与 CLI 对齐。验证：`go test ./cmd -run envExport -count=1`
- [x] 1.4 新增测试：一条好 ref + 一条 `{{text:llm-keys:missing}}` → stdout 两行、stderr 含 key 名、exit 0；循环引用 → 非 0。验证：`go test ./cmd -run 'Export.*Loose|envExport' -count=1`

## 2. RenameProvider env 级联（Phase B）

- [x] 2.1 `RenameProviderResult` 增加 `EnvRefsUpdated int`；在 `mutate` 内自有凭据 text 改名成功后，扫描 env 全部分组，精确匹配 `{{text:llm-keys:<old>}}` → `{{text:llm-keys:<new>}}`；失败回滚整次 rename。验证：`go test ./internal/llm -run RenameProvider -count=1`
- [x] 2.2 扩展 `rename_provider_test.go`：SeedRef 级联、非精确模板不改写、外部引用档案不扫描。验证：`go test ./internal/llm -run 'RenameProvider.*Env|RenameProviderOwned' -count=1`
- [x] 2.3 CLI/TUI rename 输出追加「已更新 N 条 env 引用」（`cmd/ai_provider.go`、`internal/tui/ai_tab.go`）。验证：`go run . ai provider rename --help` 不变；TUI 手动 smoke 可选

## 3. 文档与 skill

- [x] 3.1 更新 `.agents/skills/senv-cli/SKILL.md`：`env export` 缺失引用为 warning 非失败；rename 级联 env SeedRef。验证：`go run . --help` 与 skill 对照
- [x] 3.2 运行 `openspec validate env-export-loose-and-rename-cascade --strict`。验证：命令 exit 0

## 4. 集成验证

- [x] 4.1 复现用户场景：default 组含 stale `{{text:llm-keys:TokenApi}}` + 其它变量 → `eval "$(go run . env export --if-session)"` 成功且 stderr 有 warning。验证：本地 vault 或测试 fixture
- [x] 4.2 rename 后 `env export` 无该 warning、值指向新 text key。验证：`go test ./internal/llm ./cmd -count=1`
