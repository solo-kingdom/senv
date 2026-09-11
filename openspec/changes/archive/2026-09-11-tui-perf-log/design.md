# Design: tui-perf-log

## Context

见 driver design 与 grill 根因结论。现状：CLI/TUI 无任何日志库（log/slog 仅 server handler 在用）、无 pprof/trace、无计时埋点；唯一持久日志是操作审计 `~/.log/senv/audit.log`（JSON lines 追加写，`LogOp` best-effort）。审计侧已知的反面教材：`cmd.auditOp` 每条事件新建 session.Manager 并重开日志文件——耗时日志必须按进程复用句柄。

## Goals / Non-Goals

**Goals:** 低开销、可开关、可回测的耗时日志设施；本次 driver 后续子 change（net/load）直接用它的数字做验收

**Non-Goals:** pprof/continuous profiling；跨进程 trace 关联（trace id 传播）；日志轮转（审计轮转本就是 future enhancement，另行处理）；server 侧日志

## Decisions

1. **库与格式**：标准库 `log/slog` JSON handler，不引第三方依赖；每行含 `stage`（阶段标识，命名约定 `<域>.<阶段>`，如 `tui.load-env`、`cli.autopull`、`sync.collect`、`net.request`）、`duration_ms`、`ts`、`ok`、规模维度（`groups`/`items`/`bytes`/`conns` 等按阶段可选）。
2. **落盘**：`~/.log/senv/perf.log`，0600（对齐敏感文件权限 spec 精神）；进程内单例 writer，文件句柄复用、追加写，避免审计侧每事件重开的教训。
3. **阈值与开关**：默认 100ms；`SENV_PERF_THRESHOLD`（毫秒整数，非法/≤0 回退默认）；`SENV_PERF=off` 整体关闭（关闭零写放大：构造即 no-op sink）。
4. **埋点 API 形态**：`perflog.Stage(name)` 返回带 `done(size...)` 的计时器（defer 友好）+ `perflog.Note` 记录不需阈值判断的即时事件（如建连次数）；进程启动时初始化一次。TUI 全屏模式不向 stderr 输出任何内容。
5. **埋点点位（本 change 一次布好，net/load 只消费）**：
   - 启动阶段：认证（含是否命中会话缓存）、manager/provider 构造、TUI 各 Tab 首次装载、CLI 命令主路径
   - vault 全量加载：env/text/host/config 各域装载，附组数/条目数
   - 网络：auto pull / auto push / History 查询，附 `conns_new`（进程内新建连接计数，net 子 change 在 client 侧回填真值）
   - 同步扫描：`LocalSyncSnapshot`/collect，附扫描条目数
6. **失败语义**：写失败（磁盘满、权限）静默丢事件并进程内计数，MUST NOT 报错打断业务；文件不可用时 sink 退化为 no-op。

## Risks / Trade-offs

- [高频小操作刷日志] → 阈值 100ms 过滤 + 阶段粒度只到「一次装载/一次请求」，不做逐文件粒度
- [并发阶段交叉写] → slog handler 串行化单行写入；`conns_new` 等计数维度由调用方汇总后一次写入
- [轮转缺失导致文件增长] → 与审计同策略暂不处理；JSON 行便于后续按需清理

## Migration Plan

无迁移。新文件首次写入时创建。

## Open Questions

无
