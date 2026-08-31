## Context

`senv git sync` 已是「commit → pull --rebase → push」。独立的 `senv push` / `senv git push` 仍走 `AddCommitPush` → `Push`：本地提交后直接 `git push`，远程领先即 non-fast-forward 失败。

用户要的是：**push 仍以「上传本地提交」为主**，但远程有改动时先 merge 再推；冲突则停。这与 sync 的 rebase 线性历史不同，必须走单独的 merge 路径，不能复用 `Pull()`。

## Goals / Non-Goals

**Goals:**

- 所有 push 入口（`senv push`、`senv git push` 的默认/`-m`/`--only`、交互菜单「推送」）在真正 push 前，若远程有本地没有的提交，则 merge pull。
- merge 成功（含快进）后继续 push；冲突则停止且不 push、不 force push。
- 失败信息含 git 输出与数据仓路径（与现有 `withDataDir` 一致）。

**Non-Goals:**

- 不改 `senv git pull` / `senv git sync`（仍 rebase）。
- 不 stash、不自动解决 `.enc`、不 force push。

## Decisions

### D1 — 把 merge 放进 `PushWithContext`，而不是改 CLI 各分支

`AddCommitPush`、`--only`、无工作区改动时的纯推送、交互菜单都调用 `Push`。放在 `Push` 内一次覆盖全部入口；CLI 只负责提交与文案。

`Sync` 在 rebase 之后再调 `Push`：通常远程已不领先，多一次 fetch 后跳过 merge，可接受。

### D2 — merge，不用 rebase

用户明确要求 pull & merge。`Pull()` 固定 `pull --rebase`，push 新增独立步骤（例如 `pull --no-rebase --no-edit`，或 fetch 后 `merge --no-edit @{u}`）。能快进则快进，不必强行产生 merge commit。

禁止打开编辑器：必须 `--no-edit`（或等价环境），避免非交互卡住。

### D3 — 冲突：中止 merge，保留本地提交

「停止」= 不再 push。仓库恢复到 merge 前（`merge --abort`），本地未推送提交保留，避免半成品 MERGE_HEAD 让后续 senv 命令踩到冲突中的 `.enc`。错误须含数据仓路径与手动处理指引（不要 force push）。

与 sync 的 `rebase --abort` 同策略。

### D4 — 仅当「有本地可推提交」且「远程有新提交」才 merge

1. 无 remote → 现有错误。
2. 无 upstream → 仍 `push -u origin <branch>`，不 pull。
3. `fetch` 后：`@{u}..HEAD` 为空 → `ErrNothingToPush`（不 pull）。
4. 本地有提交且 `HEAD..@{u}` > 0 → merge；成功后再 `git push`。
5. 本地有提交但远程不领先 → 直接 push。

先确认有本地提交再 fetch/merge，避免「只想知道没东西可推」时去改历史。实现上可先 fetch 再判断两边，结果须与上表一致。

### D5 — 脏工作区且需要 merge 时拒绝

默认 push 会先 commit，工作区干净。`--only` / 交互推送可能带着未提交文件。此时 MUST NOT merge（以免把脏文件卷进合并），返回明确错误，与 `Pull` 拒脏一致。

## 数据流

```
senv push / senv git push [-m | --only]
        │
        ▼
  （默认/-m：有改动则 Add+Commit）
        │
        ▼
  Push
        │
   无 remote ──► error
        │
   无 upstream ──► push -u origin <branch>
        │
        ▼
     fetch
        │
   无本地可推提交 ──► ErrNothingToPush
        │
   远程无新提交 ──► git push
        │
        ▼
   工作区脏？ ──yes──► error（不 merge）
        │ no
        ▼
   pull --no-rebase --no-edit
        │
   冲突 ──► merge --abort ──► error（含路径，不 push）
        │ ok / fast-forward
        ▼
     git push（失败含 git 输出；禁止 --force）
```

## 错误处理策略

| 阶段 | 策略 |
|------|------|
| fetch / merge 失败（非冲突） | 含 git stdout/stderr；不 push |
| merge 冲突 | `merge --abort`；中文说明 + 数据仓路径；不 push |
| 脏工作区需 merge | 拒绝 merge；提示先提交或清理 |
| push 失败 | 含 git 输出（保持现有可读性要求） |
| 禁止 | `--force`、自动 resolve、打开 merge 编辑器 |

## CLI 示例

```bash
# 本地提交后远程已有新提交、无冲突：自动 merge 再 push
senv push
senv git push -m "更新配置"

# 只推已有提交，同样会先 merge
senv git push --only

# 冲突时命令失败，仓库回到 merge 前；到数据仓手动处理后再推
senv git push --only
```

## Risks / Trade-offs

- **[merge 产生非线性和 merge commit]** → 与 sync 的 rebase 并存；文档写清「push 用 merge，sync 用 rebase」。
- **[.enc 冲突无法自动解决]** → abort + 指引；不假装合并。
- **[Sync 末尾 Push 多一次 fetch]** → 通常 no-op；极端并发下可再 merge 一次，优于直接 push 失败。
- **[BREAKING：远程 ahead 时 push 不再直接失败]** → 无冲突会成功并可能多一个 merge commit；需要「失败即停、不合并」的脚本应改用裸 git。

## Migration Plan

- 发布说明：`senv push` 在远程领先时会 merge。
- 文档：`docs/git-and-interactive.md` 去掉「push 只上传、不 pull」。
- 回滚：`Push` 去掉 fetch/merge 步骤即可。

## Open Questions

- 无（按用户要求：merge；冲突停止；成功后继续 push）。
