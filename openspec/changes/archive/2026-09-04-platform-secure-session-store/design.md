## Context

见 `proposal.md - Why`：现行"仅 OS 确认 memory-backed 文件系统"策略在 macOS 无可行实现。当前实现集中在 `internal/session/cache.go`（`saveCache/loadCache/clearCache` + tmpfs/XDG fallback），有两个平台级缺口：

1. `rejectSymlinkComponents` 对原始路径逐级 Lstat，macOS 系统自带 `/var → /private/var` 即被误拒；
2. `runtimefs_darwin.go` 无法证明 memory-backed（macOS 也确实没有 tmpfs），必然 fail closed。

另一硬约束：`Manager.AuthorizeMCPRequest` 每个 MCP 请求都会重新加载并校验 cache，因此任何新后端的读取都必须**零交互、零弹窗**。

## Goals / Non-Goals

**Goals:**

- macOS `session start` 开箱即用（Keychain），MCP 每请求读取无弹窗
- Linux 现有 tmpfs 路径行为与安全等级不变
- headless macOS/CI 有一条显式、带警告的活路（磁盘逃生舱）
- 缓存 JSON 结构、timeout/boot ID/单 session 语义不变

**Non-Goals:**

- agent/daemon 模型（后续独立 change）
- 进程级隔离、Secure Enclave
- 多 vault 并行 session

## Decisions

### D1: 引入包内 `SessionStore` 抽象，按平台选择

```go
type SessionStore interface {
    Save(cache *SessionCache) error
    Load() (*SessionCache, error)   // nil, nil = 不存在
    Clear() error
}
```

选择顺序：显式逃生舱 → 平台默认（darwin: Keychain；linux: 现有 tmpfs 逻辑）。现有 `cache.go` 中 tmpfs/XDG/fallback 逻辑整体成为 `tmpfsStore`，不重写其加固属性（随机目录、0700/0600、flock、no-follow 锚定）。

*备选：直接在 cache.go 里堆 if runtime.GOOS —— 拒绝，读写路径会分叉、难测。*

### D2: Keychain 经 `/usr/bin/security` 子进程访问，不引入 Go 依赖

- item：`add-generic-password -U -s senv.session.<uid> -a senv -w <JSON>`，写入时 `-T /usr/bin/security` 使后续经 `security` 的读取静默（MCP 零弹窗）
- 读取：`find-generic-password -s senv.session.<uid> -w`；删除：`delete-generic-password`
- payload 为现有 `SessionCache` JSON，格式不变
- `security` 不存在/失败 → fail closed，映射为可行动错误

*备选：go-keychain 原生绑定 —— 拒绝：新增依赖，且本地频繁重编译会改变二进制签名导致 ACL 反复弹窗；经 Apple 签名的 `security` 子进程对"本地构建"场景更稳。*

### D3: runtime root 先可信解析、再校验

候选文件路径先 `filepath.EvalSymlinks` 解析（`/var/folders/...` → `/private/var/...`），然后对**解析后的路径**做组件无符号链接校验 + 介质探测；写入继续走 `securefs` 的 O_NOFOLLOW 目录锚定。防的仍是"用户可操控的 symlink 劫持"，不再误伤系统链接。

*备选：直接删掉预检查 —— 拒绝：丢失纵深防御。*

### D4: 逃生舱写入固定用户缓存路径，读取按序发现

- 路径：`${XDG_CACHE_HOME:-~/.cache}/senv/session.json`（刻意避开会被清理的 legacy `~/.local/share/senv/session/`），0600、原子写、boot ID 校验照旧
- `session start --insecure-cache` 开启：先向 stderr 打印醒目警告，再写入
- 读取顺序：平台默认存储 → 逃生舱文件；`session clear` 两者都清；两处同时存在有效缓存时报"multiple session caches"（与现有 fallback 多缓存语义一致）

### 数据流

```
session start ──▶ SessionManager ──▶ SessionStore.Save
                                     │
              ┌──────────────────────┼──────────────────────┐
              ▼ (darwin 默认)        ▼ (linux 默认)          ▼ (--insecure-cache)
        /usr/bin security       tmpfs store           ~/.cache/senv/session.json
        Keychain item           (逻辑不变, 修复        0600 + 原子写 + bootID
        senv.session.<uid>       symlink 解析)
              │                      │                      │
              └──────────┬───────────┴──────────────────────┘
                         ▼
              读: 平台存储 → 逃生舱;  clear: 全部清理
                         ▼
            MCP 每请求 Load+校验(零交互) ──▶ key 或撤销
```

### 错误处理策略

| 场景 | 行为 |
|---|---|
| Keychain 锁定/不可用 | 非零退出；错误含原因 + `--insecure-cache` 提示；不写任何文件 |
| `security` 缺失/异常退出 | 同上，fail closed；stderr 保留退出码摘要 |
| item 不存在 | 等价 `os.ErrNotExist`，走"无 session"路径 |
| 逃生舱文件损坏/校验失败 | 视为无 session；不自动删除（保留诊断现场） |
| 平台存储与逃生舱同时有效 | "multiple session caches" 错误，要求 `session clear` |
| 解析后路径仍含 symlink / 介质不合格 | 维持现有 fail closed |

### 兼容性

- 缓存 JSON 结构不变，无格式迁移
- Linux 行为不变；macOS 此前从未成功写入，无存量迁移
- legacy `~/.local/share/senv/session/` 清理逻辑保留；逃生舱路径与其分离，不会误清

### CLI 使用示例

```console
$ senv session start -t 8h            # macOS: Keychain；Linux: tmpfs
$ senv session start -t 8h --insecure-cache   # headless/CI，先输出警告
WARNING: session key will be stored unencrypted on disk (0600). ...
$ senv session clear                  # 清理平台存储与逃生舱
```

## Risks / Trade-offs

- [Keychain 静默读取 = 同用户可 exec `security` 的进程皆可读] → 文档明确定位为"用户态加密静态存储"；需要更强隔离的用户不上 `never`/不启用，或等 agent 方案
- [本地多版本 senv 并存] → item 以 uid 命名、单 session 语义与现状一致，新版本 upsert 覆盖
- [headless 误用逃生舱] → 警告 + 默认关闭 + 文档仅推荐 CI/SSH 场景
- [darwin CI 无法测 Keychain] → `SessionStore` 接口 + fake 单测；真机 Keychain 行为列入人工验收任务

## Migration Plan

按 tasks 顺序合入：先抽象与 symlink 修复（无行为变化）→ Keychain 后端 → 逃生舱与文档。回滚 = revert 单个 change，无数据格式变更。

## Open Questions

- 逃生舱是否还需要环境变量开关（便于 MCP 拉起场景）——可在实现期决定，不影响本设计
