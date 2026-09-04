## Why

同步状态文件部分丢失（条目快照缺失、`metadata_hash` 变空）后，pull 永远收不到修复所需的远端 delta，push 又把磁盘上仍存在的文件误判为"本地新增"（BaseRevision=0）而撞 409 —— 假冲突成为永久态，只能 `--accept-remote` 全量重建。本次事故已在真实 vault 上复现。

## What Changes

- **数据一致时自动消除假冲突**：push 收到 409 时，对本地视为"新增"（BaseRevision=0）的冲突条目拉取远端密文，若与本地字节一致，则将远端 revision 收养进本地快照并继续同步，不报冲突；metadata 同理（两端 blob 一致但快照哈希为空/失配时收养远端哈希）。
- **saveState 防退化护栏**：拒绝持久化"无对应 tombstone 却净减少 entries"或 `metadata_hash` 非空→空的状态，返回明确错误而非写坏文件。
- **状态文件增加 vault 绑定与写入来源**：记录 server 地址指纹 + vault 名 + 写入路径/pid/时间；绑定不符时拒绝复用并提示重建，来源字段用于事后取证。
- 真实内容冲突（两端字节不同）行为不变，仍走现有冲突提示与人工解决流程。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `server-sync`: 冲突检测从"凡 409 必报冲突"收窄为"仅两端数据真实不同才报冲突"；同步状态获得防退化校验与 vault 绑定。

## Impact

- `internal/provider/server.go`（pushLocked 冲突处理、collectDirty 辅助）、`server_state.go`（syncState 结构、loadState/saveState 校验）
- `internal/provider/server_client.go`（冲突后按需拉取远端条目，复用既有 Pull(0) 或按 identity 过滤）
- 旧状态文件无新字段时按"未绑定"处理，首次写入补全；不迁移、不破坏向后兼容
- 安全性：收养判断仅比较密文哈希，不解密、不触碰明文；新增字段不含敏感内容，权限维持 0600

## Non-goals

- 不修复状态损坏的原始写入者（跨进程竞态实锤需另行布控，本提案的护栏负责拦截与自愈）
- 不实现三方合并或自动解决真实内容冲突
- 不处理 config 非法文件名（`:` 被 securefs 拒绝）问题
