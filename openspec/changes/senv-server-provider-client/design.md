## Context

见 proposal.md、driver design.md（D4/D6）与 server 子 change 的协议设计。依赖 `senv-server-provider-interface` 的窄接口。

## Goals / Non-Goals

**Goals:** server provider 全链路可用（init/读写/同步/离线/冲突提示）。

**Non-Goals:** 自动合并冲突；同步的后台常驻/定时触发（v1 手动或写后即时尝试）。

## Decisions

### 缓存目录与状态

server 模式的本地缓存目录与现有 dataPath 同构；同步状态（`last_synced_revision`、dirty 标记）存缓存目录内独立状态文件，不进加密区，不含敏感内容。

### 读写路径不变

`storage.Manager` 继续承担本地缓存读写；server provider 只在 Sync 时做 pull 落盘 + 收集 dirty 条目 push。dirty 判定复用文件 mtime/显式标记（实现时取简单可靠者）。

### 冲突处理 v1

409 响应解析为冲突清单，错误信息给出条目列表与两个出口：pull 覆盖本地（放弃本地改动）或强制 push（放弃远端改动）。不引入三方合并。

### session 对齐

解锁后的 key 缓存复用现有 `internal/session`（bootid、超时、审计），server 模式不新增会话机制。

## Risks / Trade-offs

- dirty 判定误判导致漏推 → 写路径统一打 dirty 标记，同步以标记为准而非 mtime 猜测
- 大 vault 首次 pull 全量较慢 → v1 接受，协议已支持增量，后续可分页
- 断网期间 metadata 未落盘的新机器无法初始化 → 明确报错提示需网络，属预期限制
