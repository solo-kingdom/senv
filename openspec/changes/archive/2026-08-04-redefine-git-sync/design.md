## Context

当前 `senv git sync` 与默认 `senv git push` 都调用 `AddCommitPush`，不先拉取。多机改同一数据仓时 remote ahead → push 失败；`Push` 还丢弃了 git 输出。`Pull` 在有脏工作区时直接拒绝，无法「带着本地改动同步」。

## Goals / Non-Goals

**Goals:**

- `sync` = commit（如有）→ pull --rebase → push（如有）
- 冲突安全 abort；错误信息带 git 输出
- 已对齐时 sync 成功退出
- CLI 与交互菜单一致；更新文档

**Non-Goals:**

- stash、自动合并 `.enc`、force push、改变独立 `pull`/`push` 主语义

## Decisions

### D1 — 新增 `Manager.Sync`，保留 `AddCommitPush`

`Sync(ctx, message)` 编排完整流程；`AddCommitPush` 留给仍要「只上传」的 `git push`（无 `--only`）。避免把 push 偷偷变成双向同步。

### D2 — 先 commit 再 rebase（不 stash）

senv 改动本应入库。有脏文件则先 add+commit，再 rebase。无脏文件则跳过 commit。

### D3 — Pull 可在「仅有已提交、工作区干净」时 rebase

现有 `Pull` 只拒脏工作区，sync 在 commit 后调用即可。抽公共 `pullRebase` / 让 `Pull` 在干净工作区时始终可用；sync 不重复「拒脏」逻辑。

### D4 — Push：附带输出 + 「无提交」可区分

- 失败：`fmt.Errorf("push 失败: %w\n%s", err, output)`
- 新增 `ErrNothingToPush`（或布尔 API），sync 将其视为成功；`--only` 可继续返回该错误以保持提示

### D5 — 确认交互

`sync` 保持现有轻量行为：`-m` 可选，默认时间戳消息，不强制 y/N（与当前 sync 一致）。有改动时打印将提交的文件列表（可选增强，非必须）。

## 数据流

```
senv git sync [-m msg]
        │
        ▼
  HasChanges? ──yes──► Add + Commit(msg|auto)
        │ no
        ▼
     fetch
        │
   behind? ──yes──► pull --rebase
        │              │ conflict → abort → error (含路径)
        │ no           ▼
        ▼           ok
   ahead? ──yes──► push (失败含 git 输出)
        │ no
        ▼
   success + postPullSelfCheck（若发生过 pull 且有 session）
```

## 错误处理策略

| 阶段 | 策略 |
|------|------|
| add/commit | 原样返回清晰中文错误 |
| fetch/pull | 含 git 输出；冲突 abort |
| push 无提交 | sync：成功；push --only：可保留「没有需要推送的提交」 |
| push 失败 | 含 git 输出；提示可再跑 `senv git sync` 或手动处理 |
| 禁止 | `--force`、自动 resolve enc |

## CLI 示例

```bash
senv git sync
senv git sync -m "更新 feg mongo 配置"
# 交互菜单选项 4：同步 (commit → pull --rebase → push)
```

## Risks / Trade-offs

- **[.enc 冲突无法自动解决]** → abort + 指引；不假装合并
- **BREAKING：sync 不再等于只 push]** → 文档与 help 文案标明；需要只上传时用 `push`/`push --only`
- **rebase 改写本地未推送提交]** → 多机单用户可接受；不提供 merge 模式（保持线性历史，与现有 pull 一致）

## Migration Plan

- 发布说明：`sync` 语义变更
- 回滚：恢复调用 `AddCommitPush` 即可

## Open Questions

- 无（按探索结论：重定义 sync，不引入 stash）
