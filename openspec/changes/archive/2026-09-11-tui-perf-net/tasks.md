## 1. 并发安全与单例

- [x] 1.1 审计 ServerProvider 内部可变状态（节流时间戳、HTTP client、token 处理），不安全处加锁/原子化；补 `-race` 并发用例（pull+push+History 并发）
- [x] 1.2 `getSyncProvider()` 进程内单例化（`sync.Once`，含错误 memo）；TUI/CLI 全部构造路径收敛到该入口；provider 选择语义与降级路径（git 模式、auto_sync 关）不变

## 2. History 延迟加载

- [x] 2.1 History Tab `Init` 去除启动查询，改为首次激活触发；数据缓存、手动刷新绕过；加载态与降级空态（git 模式/不可达）迁移到激活路径
- [x] 2.2 启动验证：server 模式启动且不进 History Tab 时无 History 请求、无该连接建立

## 3. 埋点与收尾

- [x] 3.1 HTTP 层新建连接计数回填耗时日志 `conns_new`；pull/push/History 阶段计时接入
- [x] 3.2 验收回测：`senv env list` 与 TUI 暖启动 `conns_new ≤ 1`，复用请求耗时约 0.1s 量级（对比 grill 基线）
- [x] 3.3 `openspec validate --strict --type change tui-perf-net` 通过，全量 `make check` 通过
