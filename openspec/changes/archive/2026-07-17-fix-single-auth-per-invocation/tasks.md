## 1. 进程内鉴权复用（高优先级）

- [x] 1.1 在 `cmd/auth.go` 实现按 `(configPath, dataPath)` 键的成功 `authResult` memo；`resolveAuth` 命中直接返回，成功后写入，失败不写；提供测试用 `clearAuthMemo()`。验证：`go test ./cmd -run AuthMemo -race`。
- [x] 1.2 单测：同一路径下注入可计数的 `passwordPrompter`，连续两次需鉴权的调用（模拟 `getEnvManager` + `getTextManager`）仅 prompt 一次；错误密码不 memo，下次仍可 prompt。验证：同上测试通过。

## 2. export 非交互 / 被捕获时拒 prompt（高优先级）

- [x] 2.1 定义可识别错误（如 `ErrNeedSession`）及 stderr 文案（含 `senv session start`）；在 `resolveAuth` 对非 TTY stdin、以及 `env export` 对非 TTY stdout 短路，禁止 prompt。验证：单元/集成测无 session + 假非 TTY 不调用 prompter。
- [x] 2.2 为 `env export` 增加 `--if-session`：无 session 时空 stdout、exit 0、不 prompt；有 session 时正常 export。验证：`go test ./cmd -run ExportIfSession -race`。

## 3. 回归与举一反三

- [x] 3.1 集成测：无 session 交互式 `env export`（含引用需 text）密码只问一次且不写 session。验证：`go test ./cmd -run ExportSinglePrompt -race`。
- [x] 3.2 确认 `env get`/`text get`/`config`/`doctor`/`tui` 路径均经 memo 的 `resolveAuth`，无旁路重复 prompt。验证：`rg 'VerifyPassword|promptPassword' cmd/` 审查 + 相关现有测试仍过。
- [x] 3.3 有 session 时行为不变（零 prompt）。验证：现有 session 复用测试 + `go test ./cmd -race`。

## 4. 文档

- [x] 4.1 更新 `SESSION_USAGE.md` / README：zshrc 推荐 `session start` + `export --if-session`；说明无 session 时 eval 不再弹密码。验证：文档示例与 D5 一致，无「裸 eval 靠临时密码」表述。
