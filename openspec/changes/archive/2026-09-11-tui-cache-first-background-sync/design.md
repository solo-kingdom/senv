## Context

TUI 启动链路（`cmd/tui.go` RunE）：autoPull（阻塞，网络）→ 认证 → 构造 managers → `tea.NewProgram`。各 Tab 随后才懒加载本地工作副本（快）。server 模式下 autoPull 的 2 秒预算全部落在「用户看到界面之前」。

 vault 工作副本是唯一编辑现场与完整本地缓存；`pullLocked` 对本地 dirty 条目 skip（留给 push 乐观锁），后台拉取不会覆盖未推送编辑；`AutoPull` 的同步锁与 vault mutation 锁把它和 TUI 内写操作串行化——把拉取移进 TUI 后台是安全的。

## 设计决策

1. **扩展 `SyncSource` 而非新增接口**：它已是 cmd 注入 TUI 的唯一同步缝，与 autoPull 同一套门控（server 模式 + auto_sync）；git 模式 `Sync == nil` 自然退化为现状。新增 TUI 层自有 `PullOutcome{Applied, MetadataUpdated, Err}`，不让 provider 类型渗入 TUI 渲染层。
2. **拉取发起点在 `Model.Init`**：与各 Tab 懒加载并发；Init 时 Tab 可能读到 pull 前的数据，由 pull 完成后的全量重载收敛。
3. **`Tab` 接口加 `Reload()`**（显式契约，9 处实现：7 数据 Tab + search/help）：数据 Tab 置回 `loaded=false` 并返回 load cmd。bubbletea 只把消息路由给激活 Tab，因此非激活 Tab 的重载结果落地发生在用户下次进入该 Tab 时（`Init` 因 `loaded=false` 重新拉取）——懒加载自愈，与既有行为一致。
4. **cmd 层 Pull 不打印不退出**：`tuiSyncSource.Pull` 保留审计（成功 N 条 / 失败 / 被屏蔽拦截），错误经 `PullOutcome.Err` 回流，被屏蔽用 `errors.Is(provider.ErrClientBlocked)` 可判别；toast 与错误栏文案归 TUI 层。
5. **文案遵循 CONTEXT.md 术语表**：界面不出现泛指的「刷新」，用「同步/更新」。

## 风险与权衡

- **拉取与用户编辑竞争**：写后 push 与后台 pull 抢同一把同步锁，串行化；dirty 保护兜底。
- **用户正处于表单/向导中时重载**：各 Tab 的 `*LoadedMsg` 处理只替换列表数据并 clamp 光标，表单/向导/详情覆盖层是独立字段，不受影响。
- **失败可见性**：拉取失败常驻错误栏会打扰操作（按键即清除），与既有错误栏行为一致，不做特殊处理。
