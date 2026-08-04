## 1. Git Manager 核心

- [x] 1.1 在 `internal/git` 改进 `Push`：失败时附带 git 输出；导出 `ErrNothingToPush`（或等价），无待推送时返回该错误。验证：`go test ./internal/git -race`（含新单测或表测）。
- [x] 1.2 实现 `Sync(message)`：有改动则 add+commit → Pull（rebase）→ Push；`ErrNothingToPush` 视为成功；冲突路径保持 abort。验证：用临时 git 仓单测覆盖「远程 ahead + 本地改动 → sync 成功」与「已对齐 → 成功」。
- [x] 1.3 Pull 失败错误已含输出则保持；确认 sync 不 force push。验证：代码审阅 + 冲突用例 abort。

## 2. CLI 与交互

- [x] 2.1 重写 `senv git sync`：调用 `Sync`；更新 Short/Long；有 session 且发生 pull 时复用 `postPullSelfCheck`。验证：`go test ./cmd -race`；手动 `--help`。
- [x] 2.2 更新 `interactive_git.go` 菜单文案与 `gitSync()` 调用 `Sync`。验证：编译通过。

## 3. 文档

- [x] 3.1 更新 `docs/git-and-interactive.md`：sync 语义、多机场景、与 push 区别。验证：文档与 `--help` 一致。
