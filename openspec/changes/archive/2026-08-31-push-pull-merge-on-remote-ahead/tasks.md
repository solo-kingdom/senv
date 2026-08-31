## 1. Git Manager：push 前 merge

- [x] 1.1 在 `internal/git` 为 `PushWithContext` 增加 fetch + merge 路径：有本地可推提交且远程领先时执行 `pull --no-rebase --no-edit`（或等价 merge）；能快进则快进；禁止 `--force`。验证：代码审阅确认不调用 `Pull()`（rebase）。
- [x] 1.2 冲突时 `merge --abort`，错误含 git 输出与数据仓路径，不继续 push；脏工作区且需 merge 时拒绝。验证：`go test ./internal/git -race` 覆盖「远程 ahead 无冲突 → push 成功」与「同文件冲突 → 中止且无 MERGE_HEAD」。
- [x] 1.3 调整或替换 `TestPush_IncludesOutputOnFailure`：远程 ahead 且可合并时改为期望成功；另保留/新增一条非冲突的 push 失败仍带 git 输出的用例。验证：同上测试包全部通过。

## 2. CLI / 文档

- [x] 2.1 更新 `senv git push` / `senv push` 的 Long/help，以及 `docs/git-and-interactive.md`：写明远程有更新时会 merge，冲突则停；与 `sync`（rebase）区分。验证：`--help` 与文档一致；`go test ./cmd -race`。
