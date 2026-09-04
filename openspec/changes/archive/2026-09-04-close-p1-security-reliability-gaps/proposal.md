## Why

2026-09-03 Review 复现了 8 项 P1：rekey 可把 vault 留在混合密钥状态，路径/符号链接可越过存储边界，明文导出权限过宽，session 撤销与介质承诺失效，恶意 KDF 参数可造成持久 CPU DoS。它们直接影响秘密边界与可恢复性，应在下一补丁版优先清零。

## What Changes

- Stage 0：将 `passwd`/rekey 改为可恢复事务；遍历、索引、写入、rename、fsync 或 metadata 失败均 fail closed，启动时恢复未完成事务。
- Stage A：统一单路径段与 sync entry schema 校验；所有读写删除执行 containment 与 no-symlink 检查；敏感落盘复用 no-follow 原子写。
- **BREAKING**：text/config 新建明文导出默认 `0600`，仅显式 mode 可放宽。
- Stage B：MCP 每请求校验 session 并支持到期/clear 撤销；runtime 非 memory-backed 时默认拒绝存 key；KDF 参数派生前做版本化上下限校验。
- 以非法身份矩阵、故障注入和 crash-recovery 测试作为阶段门禁。

## Capabilities

### New Capabilities
- `rekey-recovery`: durable rekey 事务、恢复与完整可解锁不变量。
- `storage-path-safety`: 统一标识、containment、符号链接和安全删除边界。

### Modified Capabilities
- `server-sync`: 服务端/客户端双边验证 entry identity。
- `sensitive-file-permissions`: no-follow 原子写与私密明文导出。
- `text-storage`: 安全解析导出路径并支持显式 mode。
- `env-key-validation`: group/key 收紧为安全单路径段。
- `config-grouped-storage`: 校验名称、索引身份与映射一致性。
- `session-auth`: runtime 介质验证及 MCP 请求级撤销。
- `crypto-kdf`: 按 metadata 版本限制可接受迭代范围。

## Impact

影响 `internal/{storage,provider,server,env,text,config,session}`、`cmd/{passwd,mcp,text,config}` 及磁盘故障/平台相关测试；不改变 AES-GCM 算法和 server API。

## Non-goals

不纳入 P2/P3 的 CLI 正确性、AAD 迁移、MCP 权限 profile、性能优化、后台同步、CI/供应链加固；分别立项。

## 安全性分析

客户端不信任远端 metadata/entry/索引或本地路径组件；所有破坏性切换须保证旧或新状态至少一套完整可解锁。无法证明安全介质、路径归属或 KDF 成本可接受时均 fail closed，兼容 legacy `kdf_iterations=0`。
