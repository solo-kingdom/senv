## Why

配置源档案开始随同步通道跨机分发后（channel 切片），用户需要在 TUI 里回答"上一轮 pull 给我带来了什么/覆盖了什么"——尤其在本机档案被远端覆盖产生冲突提示之后。审计面板已有按时间与操作类型过滤的能力，补一个"自上次 pull 以来"的过滤视图即可回答该问题（driver D8：不开新面板）。

## What Changes

- TUI audit 面板（`auditTab`）在既有过滤预设（all/ops/sessions）基础上增加"since last pull"预设：仅显示 `LastPullAt` 之后的事件，快捷键沿用既有预设循环键，空结果显示既有空态提示
- 同步状态暴露：`ServerProvider.LocalSyncSnapshot` 补充返回 `LastPullAt`（数据已持久化在 `.senv-sync-state.json`，零网络读取），经 `tuiSyncSource` 注入 audit 面板
- pull/后台自动 pull 完成后 audit 面板按既有 `Reload` 机制刷新，`LastPullAt` 取最新值

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `operation-audit`: "tui 查看" requirement 增加"自上次 pull"过滤视图——审计 Tab SHALL 能按上次 pull 时间过滤事件

## Impact

- `internal/provider/server_auto.go`（`LocalSyncSnapshot` 返回值扩展）、`internal/tui/sync.go`（`SyncState` 增字段）、`internal/tui/audit_tab.go`（预设 + 谓词）、`cmd/tui.go`（注入）
- 纯客户端展示层；服务端、同步协议、审计文件格式均不动

## Non-goals

- 不开新 Tab / 新面板；不动 audit log 文件格式与 `operation-audit` 的事件记录要求
- 不展示"上次 pull 拉到的条目清单"明细（审计事件已含写操作痕迹，够用）
- 不动 history 面板与冲突解决器

## 安全性分析

纯本机 UI 过滤，数据源为既有本机审计文件与同步状态元数据（时间戳），无新增敏感面。
