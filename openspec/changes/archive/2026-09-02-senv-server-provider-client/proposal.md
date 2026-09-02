## Why

driver 已定：server provider 采用「本地缓存即工作副本 + push/pull 同步」模式，离线可读写。本子 change 落地 CLI 侧：HTTP client、本地缓存、同步引擎与冲突处理，让 `senv` 在 server provider 模式下完整可用。

## What Changes

- server provider 实现：接入 `provider-abstraction` 的窄接口，对接 senv-server v1 API
- 本地缓存：复用现有 `storage.Manager` 文件格式作为工作副本，维护 `last_synced_revision`
- 同步引擎：增量 pull 落盘、乐观锁 push、409 冲突的检测与用户指引（v1 人工解决）
- 离线兜底：断网时读写本地缓存照常工作，恢复后可同步
- 初始化/接入：`senv init --server` 或等价入口，拉取托管 metadata 后用 vault 口令解锁
- session 缓存在 server 模式下的语义对齐（复用现有 session-auth 机制）

## Capabilities

### New Capabilities
- `server-sync`: server provider 模式下 CLI 的初始化、读写、同步、离线与冲突处理行为

### Modified Capabilities
（无——session-auth、data-consistency 等现有 capability 行为不变）

## Impact

- `internal/provider`：新增 server provider 实现（HTTP client + 缓存 + 同步引擎）
- `cmd/`：init、sync 等命令接入 server 模式分支
- 配置：新增 server provider 连接参数（地址、token、vault 标识）
