## Why

`senv push`（及 `senv git push`）在本地提交后直接 `git push`。远程已有新提交时会被 non-fast-forward 拒绝，用户必须手动 pull 再重试。多机共用数据仓时这是常态，push 应自动尝试合并后再推。

## What Changes

- **BREAKING**：`senv push` / `senv git push`（含 `-m`、`--only`、无工作区改动时的纯推送）在真正 push 前，若远程领先或已分叉，先 `fetch` 并以 **merge** 拉取；merge 成功则继续 push。
- merge 冲突时停止：不 push、不 force push；给出数据仓路径与处理指引。
- 远程无新提交时行为不变：有本地提交则 push，否则保持现有「无可推送」语义。
- 交互菜单中的「推送」走同一路径。
- `senv git sync` 仍用 `pull --rebase`，本变更不改 sync。

## Non-goals

- 不自动解决冲突（含 `.enc`），不引入 stash。
- 不改 `senv git pull` / `senv git sync` 的现有语义。
- 禁止 force push；不做 rebase、不改写已推送历史。

## Capabilities

### New Capabilities

- `git-push`: 本地提交后的推送流程：检测远程更新 → merge pull → 冲突则停、成功则继续 push。

### Modified Capabilities

- （无）现有 `git-sync` 仍只约束 sync 的 rebase 流程。

## Impact

- `internal/git`：`Push` / `AddCommitPush` 在 push 前增加 fetch + merge pull。
- `cmd/git.go`、`cmd/interactive_git.go`：沿用同一 Manager 路径；必要时更新文案。
- `docs/git-and-interactive.md`：push 不再是「只上传」。
- 测试：远程 ahead 可 merge 时 push 成功；冲突时停止且不 push。
