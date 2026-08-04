## Why

多机同时修改加密配置仓时，`senv git sync`/`push` 只做 add+commit+push，远程 ahead 时 push 被拒；用户只能手动 pull 再 push，且失败信息常只剩 `exit status 1`。需要一个真正的双向同步命令覆盖日常多机场景。

## What Changes

- **BREAKING**：重定义 `senv git sync` 为「有改动则 commit → pull --rebase → push」；不再等同于仅上传。
- 交互菜单中的「同步」走同一语义。
- push/sync 失败时透出 git 输出；已与远程对齐时视为成功（不报错）。
- 冲突时 abort rebase，给出路径与手动处理指引；禁止 force push。

## Non-goals

- 不自动合并 `.enc` 冲突，不引入 stash 流程。
- 不改变 `senv git pull` / `senv git push` 的独立语义（push 仍可只上传）。
- 不做实时协作 / CRDT。

## Capabilities

### New Capabilities

- `git-sync`: 多机配置仓的双向同步（commit → rebase pull → push）、错误可读性与空推送成功语义。

### Modified Capabilities

- （无）现有 openspec 能力均不覆盖 git sync 命令语义。

## Impact

- `internal/git`：新增 Sync；改进 Push 错误输出与「无提交可推」语义。
- `cmd/git.go`、`cmd/interactive_git.go`：sync 命令与菜单文案。
- `docs/git-and-interactive.md`：同步用法与多机场景说明。
