## Why

最近的安全与可靠性审查确认了五个未被现有测试覆盖的边界问题：同步回滚可能掩盖恢复失败、session 与 rekey 并发可产生无效缓存、fallback session 并发可删除全部缓存、rekey journal 未约束恢复对象、递归删除无法兑现零副作用承诺。这些问题会破坏同步一致性、会话可用性或受管数据的恢复边界，应在合入当前安全补丁前修复。

## What Changes

- 让 provider cache rollback 显式报告并保留所有恢复失败，避免将部分缓存状态伪装为已回滚。
- 将 session 创建与 vault rekey 线性化，并串行化 fallback runtime 下的 cache 创建和清理。
- 将 rekey manifest entry 约束为规范的受管 env/text/config 身份，恢复时拒绝控制文件和未知布局。
- 让受管目录递归删除在任何验证或并发身份变化失败时不删除原目录中的任一条目。
- 为上述竞态、回滚失败和 journal 污染路径补充确定性测试与 release gate。

## Capabilities

### New Capabilities

- 无。

### Modified Capabilities

- `server-sync`: pull apply/rollback 失败必须可诊断且不得将部分缓存伪装为完整旧状态。
- `session-auth`: session start、rekey 与 fallback cache 生命周期必须具备明确的并发线性化语义。
- `rekey-recovery`: journal entry 和恢复操作必须限于规范的受管身份。
- `storage-path-safety`: 递归删除必须在并发身份验证失败时保持零删除副作用。

## Impact

影响 `internal/provider`、`internal/session`、`internal/storage`、`internal/securefs` 及其并发/故障注入测试；不改变加密格式、同步 API 或正常 CLI 参数。

## Non-goals

不改变 AES-GCM/PBKDF2 格式、不引入新后台服务或锁协议、不扩展 MCP 权限模型，也不处理本次审查未确认的常规重构或性能优化。

## 安全性分析

恢复、会话和删除路径在无法证明状态完整或身份仍可信时必须 fail closed。修复以同一 vault 的既有锁与可信根文件操作为边界，避免允许旧 generation、损坏 journal 或目录替换驱动新的写入或删除。