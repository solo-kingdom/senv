## Context
server 端 entries 表仅存最新密文，revision 由 nextRevision 行锁推进（internal/server/store/store.go:244-249）；push 为乐观锁批处理（handler.go POST entries，409 冲突），pull 为增量（GET entries?since=N）；client 侧 pull 落盘在 internal/provider/server.go:320-398，push 在 server.go:420-527。决策依据：driver grill.md（D4、D7）。

## Goals / Non-Goals
**Goals:** 条目级密文历史（默认 3 版）、查询端点、cli+tui 查看、单条目恢复
**Non-Goals:** 整库快照/回滚、metadata 历史、git 模式（见 proposal）

## Decisions
1. **前像留存（pre-image）**：push 事务内、应用变更前，将被修改/删除条目的当前行插入 entries_history(vault_id, kind, grp, key, ciphertext, revision, created_at)。备选「后像留存」会让最新值重复出现且删除后最新版缺失，弃。
2. **保留裁剪在写入路径**：每次插入后按 (vault_id,kind,grp,key) 保留最近 N 行（默认 3，`--history-retain` 启动参数），N≤0 视为关闭历史。备选「后台定期裁剪」引入任务调度，弃。
3. **历史查询端点** `GET /v1/vaults/{vault}/history`：`?kind=&grp=&key=` 过滤单条目，无过滤时返回 vault 级按 created_at 倒序的最近变更（带分页 limit）；响应含 ciphertext（client 本地解密），复用 Bearer 认证与 vault 隔离。
4. **恢复 = 普通写回**：client 拉取历史密文 → 本地解密校验 → 写入本地加密存储（删除条目恢复即重新创建）→ 走既有 collectDirty/Push 乐观锁路径。复用冲突检测，无新增协议。备选「服务端直接回写历史行」会绕过冲突检测且破坏单调 revision 语义，弃。
5. **时间戳取服务端写入时刻**：与 revision 一致单调，避免多端时钟漂移误导。

## 数据流
```
push: client POST entries ──▶ [事务] 旧值插入 entries_history → 应用变更 → 裁剪至 N 版
查看: client GET history ──▶ 按条目/时间过滤 ──▶ 本地解密 ──▶ cli/tui 展示（日期+对比）
恢复: client 选历史版本 ──▶ 解密写回本地 ──▶ 既有 push（乐观锁）──▶ 新 revision
```

## 错误处理策略
- 条目无历史：cli/tui 明确提示「无历史版本」，非错误退出
- 历史密文解密失败（口令变更/rekey 后的旧密文）：提示无法解密该版本并跳过，不中断其余版本展示
- 历史端点仅只读，不产生新 revision；写入失败（含裁剪）随 push 事务回滚，不留半份历史

## CLI 使用示例
```
senv history                          # vault 最近历史变更（含日期时间）
senv history env:deploy:API_KEY       # 单条目版本列表，新到旧，含与当前值对比
senv history env:deploy:API_KEY --restore 2   # 恢复到第 2 个历史版本（交互确认后走普通推送）
# TUI：条目详情内进入 History 视图，时间线选择版本，按键恢复
```

## Risks / Trade-offs
- [存储增长随写放大] → 每条目最多 N 版硬上限，且密文体积已有 512KB 上限
- [rekey 后旧密文不可解密] → 展示层明示「无法解密」，不伪造内容；不阻塞恢复其他版本
- [push 路径多一次插入] → 同事务内单行 insert，量级可接受

## Migration Plan
0003 仅加表；`--history-retain` 有默认值，存量部署无感。回滚：回退二进制后历史表不再增长，残留行无副作用。

## Open Questions
无
