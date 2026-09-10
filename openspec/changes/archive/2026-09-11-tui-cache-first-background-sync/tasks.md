## 1. 同步缝扩展

- [x] 1.1 `internal/tui/sync.go`：`PullOutcome`、`SyncSource.Pull`、`syncPullMsg`、`pullSync`；验证：`go build ./internal/tui`
- [x] 1.2 `Tab` 接口新增 `Reload`，9 处实现（7 数据 Tab 置回 loaded；search 重扫；help 为 nil）；验证：`go build ./...`

## 2. 模型接线

- [x] 2.1 `Managers.Refresh` 透传 `--refresh`；`Init` batch 追加后台拉取；验证：单测断言 Init 恰好一次 Pull 且 refresh 值透传
- [x] 2.2 `Update` 处理 `syncPullMsg` 三分支（错误栏 / 应用变更 toast+全 Tab 重载+徽标 / 零值仅徽标）；验证：`go test ./internal/tui -run TestSyncPull`

## 3. cmd 层去阻塞

- [x] 3.1 删除 RunE 阻塞 `autoPull`；`tuiSyncSource.Pull` 实现（审计保留、不打印不退出、被屏蔽可判别）；验证：`go test ./cmd -run TestTUISyncSource`
- [x] 3.2 `tuiCmd` Long 说明启动不等待网络与 `--refresh` 语义；验证：`go run . tui --help`

## 4. 测试与文档

- [x] 4.1 Tab Reload 单测（env/ai 代表）；验证：`go test ./internal/tui -run Reload`
- [x] 4.2 cmd 层 Pull 映射测试（节流零网络 / refresh 应用 / 网络错误 / 被屏蔽）；验证：`go test ./cmd -run TestTUISyncSource`
- [x] 4.3 README「TUI 模式」启动行为与同步段、SKILL.md 同步状态条目；验证：与界面文案一致
- [x] 4.4 `go test ./...` 全绿；`go run . --help`、`go run . tui --help` 语法检查
