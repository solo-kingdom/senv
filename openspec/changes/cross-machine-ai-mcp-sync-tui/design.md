## Context

- `internal/tui/audit_tab.go`：`auditTab` 已有 `auditFilterPresets`（all/ops/sessions，`f` 键循环）与自由文本过滤；数据源 `AuditSource.LoadAuditEvents()` 读 `~/.log/senv/audit.log`
- `LastPullAt` 已持久化在 vault 数据目录 `.senv-sync-state.json`（`internal/provider/server_state.go:39-40`）；`ServerProvider.LocalSyncSnapshot`（`internal/provider/server_auto.go:45-58`）零网络读该状态但未暴露 `LastPullAt`
- TUI 已有链路：`cmd/tui.go tuiSyncSource.Status()` → `tui.SyncState{Dirty, Last, Err}`（`internal/tui/sync.go:14-18`）→ `Model.syncState` 底部徽标；后台 pull 后 `reloadAllTabs` 已触发 auditTab.Reload（`internal/tui/sync.go:79-90`）

## Goals / Non-Goals

- Goals：audit 面板加"since last pull"过滤预设；`LastPullAt` 经既有注入链进入面板；后台 pull 后自动刷新取到最新时间
- Non-Goals：新面板、事件明细清单、history 面板改动

## Decisions

- D-a 时间来源：扩展 `LocalSyncSnapshot` 返回 `LastPullAt`（来自已加载的 `syncState`，无额外 IO），`tui.SyncState` 增 `LastPull time.Time` 字段，`cmd/tui.go` 构建 Managers 时传入 `newAuditTab(source, lastPull)`。不改 `AuditSource` 接口（时间不是事件流的一部分）
- D-b 过滤预设：`auditFilterPresets` 追加 `sincePull` 项，谓词 `e.Timestamp.After(lastPull)`；`lastPull` 为零值（从未 pull）时该预设显示空态提示（复用既有"无事件"提示文案，区分"无事件"与"从未 pull"两种说明）。选预设循环而非子 tab：与既有交互一致、零新快捷键
- D-c 刷新：后台 pull 完成后既有 `reloadAllTabs` 会让 auditTab.Reload 重读事件；`lastPull` 是构造时注入的快照值——pull 后需要更新，做法：`Reload` 时经 `AuditSource` 同一提供者读最新 `SyncState.LastPull`（`tuiSyncSource` 已在 Managers 中，作为可选接口注入 auditTab，避免 Model 级耦合）。实现取最小：`auditTab` 持有一个 `func() time.Time` 回调，由 `cmd/tui.go` 用 `tuiSyncSource.Status()` 填充
- D-d pull 事件本身可见：pull 成功会写 `op_sync` 审计事件（既有），其在 `LastPullAt` 之后（时间戳晚于状态写入），自然出现在视图中

## Risks / Trade-offs

- 本机时钟回拨会让"since last pull"漏掉少量事件——过滤语义按时间戳字面执行，不引入修订号比对（审计文件无 revision 字段，不值得为此扩格式）

## Migration Plan

无。

## Open Questions

无
