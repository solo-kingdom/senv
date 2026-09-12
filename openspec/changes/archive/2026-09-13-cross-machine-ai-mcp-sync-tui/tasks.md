## 1. 同步状态暴露

- [x] 1.1 `internal/provider/server_auto.go` `LocalSyncSnapshot` 返回值增 `LastPullAt`（读自 `syncState`，零值安全）；`internal/tui/sync.go` `SyncState` 增 `LastPull`；`cmd/tui.go` 注入链打通
- [x] 1.2 单测：有/无同步状态文件时快照返回值正确
- 验证：`go test ./internal/provider/ -race` 全绿

## 2. audit 面板过滤视图

- [x] 2.1 `internal/tui/audit_tab.go`：`auditFilterPresets` 增"since last pull"项，谓词按 `LastPull` 过滤；`lastPull` 经回调动态获取；零值时显示"从未 pull"空态提示
- [x] 2.2 单测（tea 模型级）：预设循环覆盖新视图；时间过滤正确；从未 pull 空态；pull 后回调返回新时间、Reload 后视图包含 pull 事件
- [x] 2.3 spec 场景逐条落测试（`specs/operation-audit` delta 四场景）
- 验证：`go test ./internal/tui/ -race` 全绿

## 3. 收尾

- [x] 3.1 `make check` 通过
