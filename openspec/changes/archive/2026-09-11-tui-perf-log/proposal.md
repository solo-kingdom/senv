# tui-perf-log

## Why

全仓 CLI/TUI 侧没有任何耗时埋点：慢在锁、解密、网络还是扫描无从证明，性能优化的验收数字也无法回测。grill 根因调查（`../tui-perf-driver/grill.md`）只能靠一次性手工测量完成；耗时日志（Perf Log）作为常态化设施沉淀下来，服务本次 `tui-perf-net` / `tui-perf-load` 的验收与后续回归。决策 D3：独立 `~/.log/senv/perf.log`、log/slog JSON lines、阈值过滤，与操作审计语义分离。

## What Changes

- 新增 client 侧耗时日志：关键路径（TUI/CLI 启动各阶段、vault 全量加载、网络同步请求、本地同步扫描）耗时超过阈值时追加 JSON 行到 `~/.log/senv/perf.log`
- 阈值默认 100ms，环境变量可调可关；关闭时零写放大
- 记录项附规模维度（组数/条目数/字节数/建连次数）与结果（成功/失败/超预算），MUST NOT 含明文值或密钥材料
- 新增埋点点位但不改变任何业务行为：日志写入失败静默降级

## Capabilities

### New Capabilities
- `perf-log`: client 侧关键路径耗时日志的记录、阈值过滤、开关与查看

### Modified Capabilities

（无——本 change 不改变任何既有 spec 级行为）

## Impact

- 新增 `internal/perflog`（或同级）包；埋点接线：`cmd/tui.go`（启动阶段）、`cmd/root.go`/`cmd/autosync.go`（CLI 命令阶段与 auto pull/push）、`internal/tui`（Tab 装载、同步状态收集）、`internal/provider`（网络请求耗时与建连维度）
- 用户可见面：`~/.log/senv/perf.log` 新文件、`SENV_PERF*` 环境变量——按仓约定同步 `.agents/skills/senv-cli/SKILL.md`
- 与 `~/.log/senv/audit.log`（操作审计）文件分离，互不影响
