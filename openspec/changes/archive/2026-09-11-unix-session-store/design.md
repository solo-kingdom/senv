## Context

见 `proposal.md`。现状：`defaultSessionStoreFor` 在 Darwin 返回 `keychainStore`（shell 出 `/usr/bin/security`），Linux 返回 `tmpfsStore`。Darwin 的 `platformRuntimeFilesystemProbe` 恒为 unknown，故去掉钥匙串后若只走 tmpfs，stock Mac 只能 `--insecure-cache`。读写路径见 ADR-0015 / ADR-0016。

## Goals / Non-Goals

**Goals:**
- 全平台同一套选型：证明 memory-backed → tmpfs；否则 Linux fail closed，Darwin 默认磁盘逃生舱
- 删除钥匙串后端与所有 `security` 调用；读/清/升级都不碰遗留 item
- Darwin 能识别 tmpfs/ramfs（fstypename），自备挂载则走安全存储

**Non-Goals:**
- 不自动创建 RAM Disk，不探测 APFS RAM Disk，不凭据代理
- 不改 cache JSON、分槽、到期判定实现

## Decisions

1. **去掉 `keychainStore`**：`defaultSessionStoreFor` 一律 `tmpfsStore`。备选：保留 `--keychain` → 否决（ADR-0015）。
2. **Darwin 写路径回退**：`saveCache` 在未开 `--insecure-cache` 时先 tmpfs；若 `ErrNoSecureSessionStore` 且 `GOOS==darwin`，打印既有逃生舱警告并写 `diskCacheStore`。Linux 仍把该错误抛给调用方。备选：Darwin 直接默认 disk、跳过探测 → 否决（自备 tmpfs 应走安全存储）。
3. **Darwin probe**：`runtimefs_darwin.go` 用 `statfs` 的 `f_fstypename` 识别 `tmpfs`/`ramfs`，其余 unknown。不把 APFS ramdisk 当 memory-backed。
4. **读路径**：只查 tmpfs + 磁盘逃生舱（含 legacy 文件槽）。多份可读缓存仍报 `errMultipleSessionCaches`。Clear / ClearAll 同样不调钥匙串。
5. **警告文案**：Darwin 默认落盘与 `--insecure-cache` 共用「派生钥明文落盘」警告；`--insecure-cache` 帮助改为「Linux/CI 显式逃生舱；Darwin 无安全存储时已是默认」。

## 数据流

```
session start
    │
    ├─ --insecure-cache ──▶ diskCacheStore ──▶ ~/.cache/senv/session-<slot>.json
    │
    └─ 默认
         │
         ├─ probe(XDG_RUNTIME_DIR | tmp fallback)
         │     tmpfs/ramfs ──▶ tmpfsStore
         │     unknown/disk ─┐
         │                   │
         ├─ linux ───────────┴──▶ fail closed（指引 --insecure-cache）
         └─ darwin ─────────────▶ 警告 + diskCacheStore
```

读：tmpfs 与 disk 都看；钥匙串不看。命中一份则用；两份都有则不可判定。

## 错误处理策略

- Linux 无安全存储且无 flag：`ErrNoSecureSessionStore`，非 0，不写盘
- Darwin 无安全存储：不报错，警告后写逃生舱；写盘失败才非 0
- 探测/statfs 失败：视为未证明（unknown），走上面两条
- `session clear --all`：只清 tmpfs 与磁盘槽；钥匙串失败不再出现
- 不可判定（多缓存、损坏 JSON）语义不变，保留缓存

## 向后兼容

- 钥匙串中 `senv.session.<uid>` / `senv.v1*` **不迁移、不删除**；该会话失效，需重新 `session start`
- 磁盘逃生舱与 tmpfs 的文件名、JSON、0600/0700 不变
- `--insecure-cache` 保留，Linux/CI 行为不变
- 到期/失效/续期不改

## CLI 示例

```bash
# stock macOS（SSH / 无 GUI）：无 flag，警告后落盘
senv session start --timeout 8h

# Linux：仍用 tmpfs
senv session start --timeout 8h

# Linux 无 tmpfs / CI
senv session start --insecure-cache --timeout 8h

# Darwin 自备 tmpfs
XDG_RUNTIME_DIR=/path/to/tmpfs senv session start --timeout 8h

senv session status
senv session clear          # 只清当前 vault 的 tmpfs/磁盘槽
senv session clear --all    # 仍不调用钥匙串
```

## Risks / Trade-offs

- [stock Darwin 派生钥落盘] → start 时警告；能证明 tmpfs 则不落盘
- [钥匙串孤儿] → 不读即无泄露面扩大；文档说明可在钥匙串访问里手动删
- [已点过「始终允许」的用户升级后会话消失] → BREAKING，需重新 start；换掉 GUI 依赖
- [Darwin 误把磁盘当 tmpfs] → 只认 fstypename，APFS/`/var/folders` 仍 unknown

## Open Questions

无

## Migration Plan

1. 发版说明：macOS 不再用钥匙串；请重新 `session start`；远程无需点击
2. 无需数据迁移脚本
3. 回滚：恢复 `keychainStore` 仍读不到本 change 写入的磁盘缓存（读路径本就同时看 disk）；旧钥匙串 item 若还在则可再被旧二进制读取
