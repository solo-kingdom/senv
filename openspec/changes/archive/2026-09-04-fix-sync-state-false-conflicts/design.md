## Context

事故根因分析表明：同步状态一旦丢失部分条目快照（如 `config_index`）且 `metadata_hash` 变空，pull 因两端一致永远收不到修复 delta，push 又把磁盘上仍存在的文件视为"本地新增"（BaseRevision=0）撞 409，形成永久假冲突。状态写入本身已是原子（temp + fsync + rename + dir fsync），缺口在"缺少一致性自愈"与"缺少退化护栏"。

## Goals / Non-Goals

**Goals:**

- 假冲突（两端密文一致、仅快照失真）在正常 `senv sync` 中无声或低噪声自愈，无需 `--accept-remote`。
- 防止未来任何写入者把状态文件写成退化形态（entries 净减无 tombstone、metadata 哈希非空→空）。
- 状态文件与 vault 绑定，杜绝同一缓存目录被多个 vault 交叉污染；记录写入来源便于取证。

**Non-Goals:**

- 不追查/修复状态损坏的原始写入者（需另行布控实锤）。
- 不做三方合并、不自动解决真实内容冲突、不改 CLI 冲突解决命令。
- 不新增 server API（自愈复用既有 `Pull(0)`）。

## Decisions

### 1. 自愈触发点：push 的 409 处理内联完成

`pushLocked` 收到 `ConflictError` 后，不再直接构造 `SyncConflictError`，而是先对冲突分类：

- **可收养**：该条目本次以 `BaseRevision==0` 推送（本地快照缺失，视为新增），且远端密文存在、`hashBytes(本地) == hashBytes(远端)` → 把远端 revision 写入 `st.Entries`，从待推送清单剔除后重试推送剩余条目（或若已无剩余则直接落状态）。
- **真实冲突**：其余情况（哈希不同、本地为删除标记而远端仍存在等）→ 维持现有 `SyncConflictError` 行为。

远端密文获取：检测到可收养候选时执行一次 `api.Pull(vault, 0)`，按 identity 过滤出冲突条目。全量拉取一次性成本换零新 API、全版本 server 兼容；vault 很大时仅在 409 已发生才触发，正常同步零开销。

**替代方案**：新增按 identity 的单条读取 API。被否决：扩大 v1 API 面，且旧 server 无法受益。

### 2. metadata 自愈：在 pull/push 的既有比较处分流

`pullLocked` 已同时持有 localMeta 与 remoteMeta：当 `st.MetadataHash != localHash` 且 `localHash == remoteHash` 时，收养 `localHash`（等于远端哈希）并视为已修复，不算冲突、不写文件。`pushLocked` 的 metadataConflict 判定同样先检查 `remoteHash == localHash` 再收养。两端一致时不产生任何网络副作用之外的动作。

### 3. 退化护栏：统一状态写入咽喉 + 白名单重建路径

`saveState` 与 `applyRemote` 的状态写入统一收敛到内部 `writeStateChecked`：

1. 读取现有状态文件（不存在则跳过校验）；
2. 若现有 entries 数 > 新状态且差集无对应删除标记 → 拒绝，返回 `ErrStateRegression` 类错误；
3. 若现有 MetadataHash 非空而新值为空 → 拒绝；
4. 通过后走既有原子写。

`bootstrapLocked` / `acceptRemoteLocked` / `migrateFromServerLocked` 属显式全量重建，允许 entries 收缩（远端确实删除/无该条目是合法结果），但仍禁止把非空 metadata 哈希写成空串；`acceptRemoteLocked` 仅在 `GetMetadata` 明确返回 `ErrVaultNotFound` 时允许空值（远端确无 metadata）。

### 4. vault 绑定与写入来源字段

`syncState` 新增两个非敏感字段：

```json
"vault_binding": {"server": "<sha256(address)前16hex>", "vault": "<vault名>"},
"written_by": {"path": "pushLocked", "pid": 12345, "ts": 1759977000}
```

- `NewServerProvider` 构造时计算地址指纹存入 provider；测试注入构造函数传合成绑定。
- 加载时：字段存在且与当前配置不符 → 明确报错并指引 `senv sync --accept-remote` 重建，不静默沿用；字段缺失（旧文件）→ 正常加载，首次成功写入时补全。
- 每次写入更新 `written_by`，用于下次事故自证（对应故障报告建议的"stateBytes 加日志"的低成本替代）。

### 数据流

```
senv sync
   │
   ▼
pullLocked ── local==remote 且 st.MetadataHash 失配? ──是──▶ 收养哈希(不写meta文件)
   │                        │否
   ▼                        ▼
pushLocked ── api.Push ── 409 ──▶ 分类冲突
   │                        │
   │              BaseRevision==0 且 hash(本地)==hash(远端)?
   │                  │是                    │否
   │                  ▼                      ▼
   │          收养远端revision进快照    SyncConflictError(现有行为)
   │          重试推送剩余条目
   ▼
writeStateChecked ── 对照现有文件 ── 退化? ──是──▶ ErrStateRegression(拒绝写)
   │                        │否
   ▼                        ▼
   └──────► 原子写(temp+fsync+rename+dirsync，附 vault_binding/written_by)
```

### 错误处理策略

- 收养过程中 `Pull(0)` 网络失败 → 返回原始网络错误，本地状态不落盘（下次 sync 重试），不误报内容冲突。
- `writeStateChecked` 拒绝 → 同步以明确错误中止，现有状态文件保持不变；错误信息包含丢失条目数与建议（`--accept-remote`）。
- vault 绑定不符 → 加载即失败，提示当前绑定与文件内绑定，指引重建；绝不静默覆盖。
- 旧状态文件（无新字段）完全兼容；`json.Unmarshal` 忽略未知字段保证前向兼容。

### 向后兼容与回滚

- 状态文件只增字段：旧版二进制读取新文件时忽略未知字段（Go json 默认行为），可安全降级运行（降级期间自愈与护栏失效但不产生新损坏）。
- 回滚 = 替换二进制；已补全的绑定/来源字段对旧版无害。
- 无 CLI/网络协议变更，server 端零改动。

## Risks / Trade-offs

- [Pull(0) 在大 vault 上一次性成本较高] → 仅在 409 且存在可收养候选时触发；正常路径零开销。后续可演进单条 API。
- [护栏可能拦截合法的大规模远端删除同步] → 仅正常 pull/push 路径执行严格校验；显式重建路径（bootstrap/accept-remote/migrate）允许收缩。
- [vault 绑定使同一缓存目录无法再被多 vault 复用] → 这正是要堵死的交叉污染面；需要切换时显式重建。
- [自愈静默可能掩盖状态损坏的再次发生] → `written_by` 提供取证线索；自愈计数在 sync 输出中低噪声提示（"已自动修复 N 条同步状态"）。

## Migration Plan

无需数据迁移：旧状态文件首次成功同步时自动补全绑定字段。发布后建议在真实 vault 上重放故障报告的复现序列验证自愈。

## Open Questions

（无）
