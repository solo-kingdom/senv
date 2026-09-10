## Why

`senv tui` 在进入界面前同步执行 autoPull（server 模式 2 秒预算；`--refresh` 强制绕过节流），网络慢或不可达时用户面对黑屏等待。而 vault 工作副本本身就是完整的本地加密缓存，可以先渲染——用户要求 TUI 优先展示本地缓存数据，数据同步后再更新展示。

## What Changes

- 移除 TUI 启动前的阻塞 autoPull：界面立即用本地工作副本渲染，`SyncSource` 新增后台拉取（沿用 2 秒预算与节流窗口）。
- 后台拉取应用了远端变更（条目或 metadata）时：toast「已从 server 更新 N 条」+ 所有 Tab 重载本地工作副本；无变更或零网络跳过（节流/锁忙）只更新同步徽标。
- `--refresh` 语义收窄为「启动后台拉取绕过节流窗口」：TUI 从不因网络阻塞（用户已确认该决策）。
- client 被屏蔽（`ErrClientBlocked`）在 TUI 内不退出进程：错误（含重新注册指引）进底部错误栏；审计口径与命令行一致。
- `Tab` 接口新增 `Reload()`：各数据 Tab 置回懒加载标记；非激活 Tab 在下次进入时经 Init 自愈，激活 Tab 立即恢复。本地 dirty 条目保护不变（pull 侧 skip dirty）。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `tui-viewer`: TUI 启动命令增加缓存优先、后台同步行为；同步状态可见性扩展启动后台拉取与应用变更后的界面更新。

## Non-goals

- 不做 TUI 打开期间的周期轮询（仅启动一次后台拉取；写后 push 已有）。
- 不改 History Tab 的按需网络加载。
- 不改 git 模式与 auto_sync 关闭时的行为（`Sync == nil`，无拉取无徽标）。
- 不改模型目录（models.dev）缓存机制（仍由 `senv ai refresh` 管理）。

## Security Analysis

- 后台拉取复用 `AutoPull` 既有路径：同步锁 + vault mutation 锁串行化，`pullLocked` 对本地 dirty 条目 skip 不覆盖——未推送的本地编辑不被远端覆盖。
- 被屏蔽语义不弱化：屏蔽仍触发 provider 侧本地清理（session cache），TUI 内以错误栏呈现指引而非静默；审计事件保留（成功/失败/被屏蔽拦截）。
- TUI 进程不再因 pull 失败 `os.Exit`：错误只影响提示条，不影响已落盘本地数据。

## 验证记录

- `go test ./...` 全绿（含新增：syncPullMsg 三分支、Tab Reload、`tuiSyncSource.Pull` 节流/refresh/错误/被屏蔽映射）。
- `go run . tui --help` 含新的启动行为说明。
