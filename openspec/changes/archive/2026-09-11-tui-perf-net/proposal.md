# tui-perf-net

## Why

到 senv-server 的每条新连接要花 0.35~1.5s（TLS 握手段；同连接后续请求仅约 0.1s，见 grill 实测）。而 `getSyncProvider()`（cmd/provider.go）没有进程内 memo，每次调用都构造新 provider（即新 HTTP client）：TUI 启动时 History Tab 与后台 pull 各建独立连接、各付一次握手；CLI 命令的 auto pull 与退出 AutoPush 同理。grill 决策 D6-A/D8③：client 侧单例化让整个进程复用一条连接，History 延迟到激活才查询。

## What Changes

- `getSyncProvider()` 改为进程内单例：同一进程内所有同步动作（TUI 的 pull/push/History，CLI 的 auto pull/AutoPush/显式 sync）复用同一 provider 实例及其 HTTP 连接
- History Tab 延迟加载：启动不发起 History 查询，首次激活该 Tab 时才查询（启动建连数随之下降）
- provider 选择语义、构造失败报错、被屏蔽（ErrClientBlocked）处理等外部行为不变
- 接入 `tui-perf-log` 的网络埋点并回填 `conns_new` 真值

## Capabilities

### New Capabilities

（无）

### Modified Capabilities
- `provider-abstraction`: 「统一构造入口」新增进程内单例与连接复用要求（同一进程复用同一实例，选择语义与构造失败行为不变）
- `tui-viewer`: History Tab 启动不查询，首次激活才加载

## Impact

- `cmd/provider.go`（memo 化）、`cmd/tui.go` 构造段与 `internal/tui/history_tab.go`（延迟触发）、`internal/provider`（并发安全审计，必要时加锁）
- 行为兼容：git 模式、未配置 provider、auto_sync 关闭等降级路径不变
- 无数据迁移、无新命令/flag
