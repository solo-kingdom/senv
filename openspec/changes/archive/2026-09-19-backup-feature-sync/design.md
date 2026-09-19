## Context

收集/落地在 `internal/provider/server_state.go`，白名单在 `internal/syncschema`（client 与 `internal/server/store` 共享）。`backups/` 由 core 建好。动机见 proposal.md - Why；D4/发布顺序见 driver design。

## Goals / Non-Goals

**Goals:**

- `backup`/`backup_meta` 走通收集 → push → server 校验 → pull → 落地
- 冲突对齐 text，不用 SSH 元数据-only

**Non-Goals:**

- git 模式改动；TUI；per-kind 开关；server 业务代码（只共享 schema）

## Decisions

### D1 kind 命名

`backup` / `backup_meta`，身份矩阵与 `text` / `text_meta` 相同。备选单 kind 无 meta 否——组说明无法同步。

### D2 收集/落地复制 text 目录遍历

`backups/{group}/.meta.enc` → `backup_meta`；`backups/{group}/{key}.enc` → `backup`。目录缺失静默跳过。

### D3 冲突按 text 解码，上限 `MaxBackupSize`

`internal/conflict/merge.go` 对 backup 走与 text 相同的内容合并路径，超限拒绝合并。备选元数据-only 否——产品要求对齐 text，且无私钥红线。

## 数据流

```
backups/{g}/.meta.enc ──┐
backups/{g}/{k}.enc ───┤ collect ── push ── ValidateIdentity
                       │
远端 ◀── pull ── land ─┘  → 原路径 0600
```

## 错误处理策略

- 未知 kind / 非法身份：整批拒绝（server 事务前）
- 合并后 value > `MaxBackupSize`：拒绝该次合并，保留冲突
- git 模式：无白名单错误面

## 向后兼容

- 旧 client 不收集新 kind，行为不变
- 新 client + 旧 server：含 backup 的整批 push 失败 → server 镜像必须先发
- 回滚 client 后 server 上 backup 条目为零知识孤儿

## Migration Plan

1. 先发布含新白名单的 senv-server 镜像（`docs/senv-server.md`，优先 iship）
2. 再发 client
3. 回滚 client 即停止新 kind 推送

## Risks / Trade-offs

- [整批同步失败被当成故障] → 发布说明 server 先行；错误沿用未知 kind 文案
- [冲突 UI 展示 backup 明文] → 与 text 同等：仅冲突解决器、需 vault key；列表仍无 value

## Open Questions

无。
