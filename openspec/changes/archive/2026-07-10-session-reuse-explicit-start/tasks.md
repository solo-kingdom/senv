## 1. Config key 路径

- [x] 1.1 在 `internal/storage` 增加 `LoadConfigFileWithKey` / `SaveConfigFileWithKey`，password 版改为 derive 后委托（与 env/text 同构）
  - 验证：`go test ./internal/storage/ -count=1` 通过；新增 key 读写单测
- [x] 1.2 在 `internal/config` 增加 `key` 字段与 `NewManagerWithKey`，load/save 按 key/password 分支
  - 验证：`go test ./internal/config/ -count=1` 通过；用 key 创建的 Manager 能读写已有 config

## 2. 去掉隐式 StartSession（高优先级）

- [x] 2.1 从 `getEnvManager` / `getTextManager` 移除密码成功后的 `StartSession(settings...)`
  - 验证：无 session 时 `env get` 成功后 `session status` 仍无 active
- [x] 2.2 从 `getManagersAt`（TUI）与 `runInteractive` 移除隐式 `StartSession`
  - 验证：无 session 进 TUI/interactive 后 cache 不被创建

## 3. 全入口复用 session

- [x] 3.1 `getConfigManager`：先 `GetCachedKey`，命中则 `NewManagerWithKey`；未命中再密码且不落盘
  - 验证：有 session 时 `config list` 不 prompt；无 session 时要密码且不写 cache
- [x] 3.2 `getManagersAt`：先 session 构造三 Manager；未命中再密码临时认证
  - 验证：更新 `cmd/tui_test.go`——有 cache 时 stub prompter 不被调用；无 cache 仍校验密码
- [x] 3.3 `runInteractive`：先 session；未命中再密码临时认证
  - 验证：有 session 时 interactive 启动不 prompt（可用手工或抽函数单测）

## 4. 文档与回归

- [x] 4.1 更新 `SESSION_USAGE.md`（及 README 中相关句）：功能命令不再自动开 session；仅 `session start` 落盘；never cache 路径说明与代码一致
  - 验证：文档示例与 `session status` / 落盘路径一致
- [x] 4.2 跑 `make test`（或 `go test -race ./...`）确认全绿
  - 验证：无失败用例
