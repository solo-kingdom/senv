# Design: server-provider-auto-sync

## Context

server provider 已实现完整的增量同步内核（`internal/provider/server.go`）：`pull` 按 `last_synced_revision` 增量落盘且保护本地 dirty 条目，`push` 收集 dirty 乐观锁批量推送。缺的只是调用时机与无感化外壳。手动同步内核的行为契约见 `openspec/specs/server-sync/spec.md`，本设计不改动其语义。

约束：无 daemon；读写不得因 server 不可达失败；本地缓存是唯一工作副本；同机可能并发多个 senv 进程。

## Goals / Non-Goals

**Goals**

- server 可达时，日常读写命令顺带完成双向同步，积压窗口收敛到秒级
- 网络异常时行为与今天的纯手动模式一致（静默降级 + 警告）
- 同机并发命令不损坏同步状态

**Non-Goals**

- 不做自动冲突合并、不引入常驻进程、不改 git provider、不改 server 端 API（见 proposal 非目标）

## Decisions

### D1: 同步时机——pull 显式接线，push 用 root PostRun 兜底

- **pull 必须发生在返回数据之前**，只能显式接到读命令入口（RunE 开头调用 `autoPull`），无法全局拦截。
- **push 用 root 命令的 PersistentPostRun** 统一兜底：命令结束后扫描 dirty（本地哈希对比，无网络），有才推送。无需枚举所有写命令，新写命令自动获得该行为；读命令 dirty 为空时零开销（一次本地扫描）。
- 备选：每条写命令显式接 push——接线点多、易漏新命令，放弃。

### D2: 节流用 syncState 持久化时间戳，默认窗口 30s

`syncState` 增加 `last_pull_at`（unix 秒）。pull 前检查：`now - last_pull_at < throttle` 则跳过。跨进程生效（每个 CLI 都是短命进程，内存态节流无效）。默认 30s，settings 可调；`--refresh` 绕过。

### D3: 互斥用 dataPath 下锁文件 + 非阻塞 Flock

同步段（pull/push/状态更新）持有 `.senv-sync.lock` 的排它锁；拿不到锁说明另一进程正在同步，**直接跳过本次同步**（对方已在做，dirty 会留在本地等下次）。锁随进程退出自动释放，崩溃安全。同步状态文件读写全部移入锁内。

### D4: 关键写单独走阻塞推送通道

`passwd`、init 后首次写入调用 `PushBlocking`（更长预算 10s，结果必须确认）；失败输出强警告（其他设备同步前拿不到新口令）但本地更改保持生效。普通写仍走 PostRun 的 best-effort 通道。

### D5: 配置挂在 ProviderConfig，指针类型表达"默认开"

```json
"provider": { "type": "server", "address": "...", "token": "...",
              "auto_sync": true, "sync_throttle": "30s" }
```

`AutoSync *bool`（nil = 默认开启，仅显式 `false` 关闭）；`SyncThrottle string`（同 session timeout 风格，默认 `30s`）。settings 是机器本地文件、从不同步，无迁移问题。

### D6: 命令层新增 `cmd/autosync.go` 薄壳

`autoPull(cmd, refresh bool)` / `postRunAutoPush()` 两个入口，内部构造 server provider（复用 `getSyncProvider`），非 server provider 或 `auto_sync=false` 时为 no-op。MCP `serve` 长驻进程复用同一对函数（节流使其每请求近零成本）。

## 数据流图

```
读命令 (env export / list / text show / session / TUI 打开)
  RunE 开头: autoPull
    ├─ provider != server 或 auto_sync=false ────────► 直接执行命令（零网络）
    ├─ now-last_pull_at < throttle 且无 --refresh ───► 直接执行命令（零网络）
    └─ 需拉取: flock(非阻塞) → pull(≤2s 超时)
         ├─ 成功: 更新 last_pull_at/last_synced_revision → 命令返回最新数据（提示更新条数）
         └─ 超时/不可达/锁忙: 静默跳过 → 命令返回本地缓存

写命令 (env set / text 写 / config 编辑 / ...)
  RunE: 只写本地缓存，立即返回结果
  root PersistentPostRun: postRunAutoPush
    ├─ 非 server / auto_sync=false / dirty=0 ────────► 静默退出
    └─ flock(非阻塞) → push(≤2s 超时)
         ├─ 成功: 无感完成（可静默）
         ├─ 网络/超时: 输出 "⚠ N 条待推送，恢复后自动重试" → 命令仍成功
         └─ 409 冲突: 输出冲突条目 + "senv sync" 指引 → 命令仍成功，dirty 保留

关键写 (passwd / init 后首次写入)
  RunE 内: PushBlocking(≤10s) → 失败输出强警告，本地更改已生效
```

## 错误处理策略

| 失败点 | 处理 | 用户可见 |
|---|---|---|
| pull 超时/不可达 | 静默跳过，节流时间戳不更新（下次仍会尝试） | 无 |
| push 网络/超时 | dirty 保留，下次命令自动重试 | 一行 ⚠ 警告 |
| push 409 冲突 | dirty 保留，两端不覆盖 | ⚠ + 冲突条目 + sync 指引 |
| 锁被占用 | 跳过本次同步（并发方在做） | 无 |
| sync state 损坏 | 视为空状态重建（首条命令会全量比对修复），不阻断命令 | 无 |
| 关键写推送失败 | 本地生效 + 强警告 | 明确告警 + sync 指引 |

原则：**任何同步层失败都不得改变命令本身的退出码与结果**（关键写除外，但也不回滚本地更改）。

## 存储格式向后兼容

- `syncState` 仅新增可选 JSON 字段（`last_pull_at`），旧文件缺字段反序列化为零值=立即拉取，旧版本二进制读到新字段会忽略。双向兼容。
- settings 新字段同为可选，旧版本忽略；新版本读旧 settings 走默认值。
- 无加密格式、缓存文件布局、server API 变更。

## 命令行接口变更示例

```console
# 读：节流窗口外自动拉取
$ senv env export prod
✓ 已从 server 更新 3 条
export FOO=...

# 读：强制绕过节流
$ senv env export prod --refresh

# 写：推送失败不阻塞命令
$ senv env set prod FOO=bar
✓ 已写入 prod/FOO
⚠ 1 条待推送（server 不可达，恢复后执行任意命令自动重试）

# 写：推送冲突
$ senv env set prod FOO=bar
✓ 已写入 prod/FOO
⚠ 1 条推送冲突：env/prod/FOO（远端已更新），运行 senv sync 解决
```

## Risks / Trade-offs

- [慢网络下读命令尾部延迟] → 2s 超时预算 + 30s 节流，P99 增量可控；`--refresh` 与 `auto_sync=false` 提供逃生口
- [误操作被自动快速传播到所有机器] → server 端保留 revision 历史，可回滚；极端场景 `auto_sync=false` 回手动模式
- [Flock 在非 POSIX 平台不可用] → 当前目标平台 linux/macOS；Windows 后续可用 O_EXCL 轮询锁替换，接口已隔离
- [PostRun 对每条命令做 dirty 扫描] → 纯本地文件哈希，vault 规模下毫秒级；dirty=0 时无网络
- [口令变更窗口期他机不可解锁] → 关键写阻塞推送 + 强警告，窗口仅为推送时长

## Migration Plan

1. 合入后 server provider 默认启用自动同步；旧 `senv sync` 行为不变，可随时手动干预
2. 回滚：settings 设 `auto_sync: false` 即回到现状，无需降级版本
3. server 端零变更，新旧客户端混跑安全

## Open Questions

（无——节流默认 30s 与超时预算 2s/10s 作为初始值落地，实测后可调，不影响 spec 与任务拆分）
