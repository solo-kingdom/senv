## Why

有效 session（含 `-t never`）存在时，`senv tui` / `config` / `interactive` 仍每次要密码；输完后还会按 settings 默认超时（如 `8h`）隐式 `StartSession`，覆盖并缩短已有 session。tmux 新窗口开 TUI 时必现，且会破坏显式开启的长期 session。

## What Changes

- 所有需解密的入口（env / text / config / tui / interactive）：有有效 session 则复用 derived key，不再要密码
- 无 session 时，功能内输密码仅作本次进程临时认证，**不**写入或刷新 session cache
- **仅** `senv session start` 创建/更新 session cache
- 为 config 补齐 key 路径（`NewManagerWithKey` + storage `*ConfigFileWithKey`），使 TUI/config 能吃 session
- settings 默认超时保持 `8h`，不改为 `never`

## Non-goals

- 不改变 session 超时语义（duration / restart / never）与 cache 落盘路径
- 不把 settings 默认改为 `never`
- 不在 session cache 中存储明文密码
- 不改加密算法或数据文件格式

## 安全性分析

- Session 仍只缓存 derived key（与现网一致）；功能内临时认证的密码仅存于进程内存，退出即丢
- 去掉隐式 `StartSession` 后，不会因「用一次功能」意外延长或缩短磁盘上的 session 生命周期；显式 `session start` 仍是唯一落盘入口，攻击面更清晰

## Capabilities

### New Capabilities

- `session-auth`: 统一「有 session 则复用；无 session 则临时密码且不落盘；仅 session start 写 cache」的认证契约

### Modified Capabilities

- `tui-viewer`: TUI 启动鉴权改为优先复用 session，无 session 时密码仅临时有效且不 StartSession

## Impact

- `cmd/tui.go`、`cmd/env.go`、`cmd/text.go`、`cmd/config.go`、`cmd/interactive_main.go`：鉴权与隐式 StartSession 行为
- `internal/config`、`internal/storage`：补齐 WithKey
- `SESSION_USAGE.md` / README 中与「命令自动开 session」相关的描述需同步
