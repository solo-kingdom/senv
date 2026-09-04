## Context

见 proposal.md。当前实现已有 vault mutation lock、trusted-root 文件操作、可恢复 rekey journal 与 session runtime 介质检查，但它们在跨边界组合时仍存在未线性化的窗口：provider rollback 丢失恢复错误，session start 以多个独立 vault 观察组成 cache，fallback 清理无互斥，journal recovery 只校验通用 segment，RemoveTree 在预检后逐项删除。

## Goals / Non-Goals

**Goals:**
- 令一次成功的 session start 与单个有效 vault generation 原子对应。
- 让同步失败的返回错误准确反映 rollback 是否完整。
- 让 recovery 的所有读写对象都可由 manifest 的业务身份证明。
- 将递归删除的“可验证失败零副作用”边界前移到不可重定向的准备阶段。

**Non-Goals:**
- 不改变 session 文件格式、rekey 主数据格式、server API 或 CLI 参数。
- 不承诺在已成功进入物理删除阶段后所有磁盘 I/O 错误都可回滚；只将验证/身份替换失败排除在原目录删除之前。

## Decisions

### 1. 将 session start 的认证与 cache 提交放入同一 vault lock lease

session start 将通过 `WithVaultMutation` 使用 lock-held storage manager，在 callback 内完成 metadata 读取、KDF 验证、口令验证、派生、最终 key 验证和 cache commit。cache commit 成功才返回成功；rekey 在 lease 外等待，避免旧密码验证与新 metadata 混合。

这比在开始和结束分别比较 salt 更可靠：比较方案仍允许两次检查之间写入无效 cache，且会增加 retry 分支。

### 2. fallback cache 生命周期使用 runtime-root 内的每用户互斥

无 XDG 时，先验证 temp runtime，再以该可信根下的固定非秘密 lock 串行化候选目录创建、写入、枚举和清理。锁持有者只删除已证明不是当前提交结果的私有目录；任何不确定的 ownership、mode 或 identity 使操作失败而不清理。

不采用“扫描后保留字典序最新目录”：它不能防止另一个并发 start 在扫描后提交，也会把时间排序误作提交顺序。

### 3. manifest 持久化 kind，恢复时重建规范身份上下文

manifest entry 增加受限 kind；预检以规范 env/text/config 身份和有效 config index 生成 entry。加载 manifest 时，在任何 sidecar/original 读写前重新验证 kind、segment 布局、规范后缀和 config-index 对应关系，并显式拒绝 state、lock、manifest 与 sidecar 名称。未知或不一致 journal 保留材料并返回 recovery-required。

保留 hash 校验作为 generation 完整性证明；仅 hash 不足以证明恢复对象属于 vault 数据模型。

### 4. provider rollback 累积所有恢复错误

rollback 为每个 snapshot 分别记录父目录准备、原子恢复、删除与 root 打开错误，使用 joined error 返回。前向错误与 rollback error 同时保留；只有无恢复错误时才可声明旧缓存已恢复。sync state 始终最后写入，任何路径均不推进 revision。

不尝试在 rollback 失败后继续提交 state 或自动重试，因为此时本地缓存是否完整不可判定。

### 5. RemoveTree 先 quarantine，再按已验证 descriptor 删除

递归删除先通过可信 parent handle 完整预检目标树，验证 source identity 后在同一 parent 内原子 rename 到不可预测的私有 quarantine 名称，再打开并复核 quarantine inode 与预检对象一致。只有该准备成功才删除 quarantine 内容；预检或准备中的身份变化不会删除原目录对象。清理成功后 fsync parent；quarantine 删除失败返回可诊断错误且不沿链接访问。

不继续采用“预检后顺序 unlink”：逐项复核虽能拒绝替换，但无法撤销此前已删除的条目。

### Data Flow

```text
session start                 pull apply                     rekey recovery / RemoveTree
     │                            │                                      │
     ▼                            ▼                                      ▼
vault lock lease            snapshot + apply                 validate canonical identities
     │                            │                                      │
verify → derive → key check  forward failure? ─ no ─► state commit       preflight trusted tree
     │                            │ yes                                  │
cache lock → cache commit    restore every snapshot                    quarantine rename + inode check
     │                            │                                      │
return valid cache           joined rollback result                    descriptor-relative cleanup
```

## Error Handling Strategy

| 失败类别 | 行为 | 状态保证 |
| --- | --- | --- |
| session/rekey 竞争 | 等待同一 vault lock 后重新观察 generation | 成功 cache 与一个有效 generation 匹配 |
| fallback lock/ownership 不可验证 | 返回错误，不清理候选目录 | 不删除其他有效 cache |
| provider forward + rollback 失败 | 返回 joined diagnostic，state 不提交 | 不伪称完整 rollback |
| journal kind/layout/index 不一致 | 返回 recovery-required，保留材料 | 不触及控制或未知文件 |
| 删除预检/准备身份变化 | 返回 boundary error | 原目录条目零删除 |

## Risks / Trade-offs

- [session lock 持有期间包含 cache I/O] → cache 写入很小且低频；失败立即释放 vault lock。
- [fallback lock 文件生命周期] → 仅在已验证 memory-backed trusted root 内创建，并拒绝 symlink/非普通文件。
- [manifest schema 演进] → 使用显式版本；旧未完成 journal 无法证明 kind 时 fail closed 并提示新版 doctor。
- [quarantine 残留] → 失败时保留在受管根内并返回清晰诊断；后续安全 recovery/cleanup 可处理。

## Migration Plan

1. 先实现并测试 provider rollback error 传播与 manifest kind 校验；旧正常数据与已完成 rekey 不受影响。
2. 部署 session lock 线性化；已有 cache 格式继续读取，fallback 旧目录仅在持锁并通过所有权/权限验证后清理。
3. 部署 quarantine RemoveTree；无长期格式变化。
4. 回滚二进制前确认不存在需要 kind-aware recovery 的未完成 journal；若存在，保留 journal 并使用新版本恢复，不能由旧版本猜测处理。

## Open Questions

无。