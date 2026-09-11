# Design: tui-perf-net

## Context

`getSyncProvider()`（cmd/provider.go:19-43）每次调用读 settings 并 `provider.New(cfg)`，无进程内 memo。TUI 启动路径上至少两处独立构造：`buildTUIHistorySource()`（cmd/tui.go:211-229）与 `newTUISyncSource()`（cmd/tui.go:115-121 → `getAutoSyncServerProvider` → `getSyncProvider`）。实测每条新连接 0.35~1.5s，同连接复用请求约 0.1s。History Tab 当前在 `Init` 即发起查询（internal/tui/history_tab.go:87-104），随 `Model.Init` 的 `tea.Batch` 并发触发。

## Goals / Non-Goals

**Goals:** 进程内 ≤1 次新建连接；History 查询退出启动路径；不改任何 provider 选择与错误语义

**Non-Goals:** 服务端 TLS 握手慢的排查（driver D6-C）；连接预热/keep-alive 心跳；pull 节流窗口语义调整（属 server-sync 既有行为）

## Decisions

1. **单例位置在 `getSyncProvider` 而非 `provider.New` 内部**：保持 `provider.New` 纯构造语义（可测试、可多实例）；`cmd` 层用 `sync.Once` 包住「读 settings + 构造」，错误同样 memo（同进程内配置不再重读——settings 在进程生命周期内视为不变，与现状「每命令一进程」等价）。
2. **并发安全先行**：单例化后 ServerProvider 会被 pull/push/History 多 goroutine 并发使用。先审计其内部可变状态（节流窗口时间戳、client、token 刷新等），不安全处以互斥保护或改为原子字段；补 `-race` 并发用例后再启用单例。
3. **History 延迟加载**：Tab 仍注册（序号与导航不变），`Init` 不发请求；引入「首次激活」触发点（Tab 切换路径发 `historyLazyLoadMsg`），数据缓存后复用；手动刷新（既有快捷键）绕过缓存。git 模式 / server 不可用的降级分支移到激活时判定，不再影响启动。
4. **埋点回填**：在 ServerProvider 的 HTTP 层（Transport 包装或请求封装处）统计进程内新建连接数，回填 `tui-perf-log` 的 `conns_new` 维度；阶段计时沿用埋点 API。
5. **TUI 退出后的 AutoPush**：与 TUI 同进程（PersistentPostRun），单例化后自动复用连接，无需单独处理。

## Risks / Trade-offs

- [settings 进程内不重读，长驻进程（TUI/MCP serve）改配置不生效] → 与现状一致（MCP serve 亦长驻）；确需生效即重启进程，spec 场景已按「新进程生效」约定
- [单例错误 memo 导致瞬时故障（如临时读不到 settings）被进程记住] → 只 memo 构造结果不重试的语义与现状每次重读略有差异；接受（现状 CLI 每命令一进程，无重试路径；TUI 启动失败本就退出）
- [History 延迟后首次激活等待] → 查询有既有预算与错误栏提示；Tab 内展示加载态

## Migration Plan

无迁移。

## Open Questions

无
