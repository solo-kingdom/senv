# Design: tui-perf-driver

## Context

grill 已收敛：`grill.md` 的 D1–D8 全部 settled、无未决问题，术语「耗时日志（Perf Log）」已沉淀至 `CONTEXT.md`。根因经实测定位（详见 grill.md「根因结论」）：R1 网络建连慢且不复用（`getSyncProvider` 无进程内 memo，TUI 启动 History 与 pull 各建独立 HTTP client，每条新连接 TLS 0.35~1.5s）；R2 全量重复读 + 每文件「flock + rekey 清算 + manifest 重读 + 开/关 config root」固定税（519 个密文文件 × 2~3 趟，实测单趟 1~1.5s、CPU 仅 0.03s）；R3 pull 应用变更后 `reloadAllTabs` 把列表清回占位再全量重读。driver 无代码变更，只编排 3 个子 change；实现细节下沉到各子 change 的 `design.md`。

## Goals / Non-Goals

**Goals:** 按修复方向切成 3 个可独立 apply/validate 的子 change；固定实施顺序；把验收数字（D8）落到各子 change 并可回测

**Non-Goals:** 冷启动 PBKDF2 路径（D1）；服务端 TLS 建连慢的 infra 排查（D6-C，拆独立调查）；同步协议与 server API 语义变更；实现层决策（见各子 change design）

## Decisions

1. **按方向切片**（对应 grill 决策编号）：
   - `tui-perf-log`：耗时日志设施——`~/.log/senv/perf.log`（log/slog JSON lines），阈值默认 100ms、env 可调/可关，埋点覆盖启动各阶段、vault 全量加载、网络请求（含连接复用维度）、同步扫描（D3/D8④）。最先实施：为后续两个子 change 提供验收度量。
   - `tui-perf-net`：client 侧网络收敛——`getSyncProvider` 进程内单例、连接复用，History Tab 延迟到激活才查询（D6-A/D8③）。
   - `tui-perf-load`：读路径收敛——单趟快照多消费方共享、锁与 manifest 税批量化、同步状态增量收集、reload 改 stale-while-revalidate（D5/D6-B/D7/D8①②）。
2. **实施顺序**：log → net → load。log 先建度量基线；net 体量小、独立见效；load 最重且其收益需靠 log 回测。
3. **capability 布局**：新增 `perf-log`；复用既有 `provider-abstraction`（统一构造入口/连接复用）、`tui-viewer`（History 延迟、单趟快照、SWR reload）、`rekey-recovery`（读路径批量清算与 manifest 缓存语义）、`server-sync`（同步状态增量收集）。
4. **跨子 change 约束**：
   - 读路径优化 MUST NOT 削弱 rekey 混合密钥代际隔离（grill ADR 候选 adr-读路径锁语义，由 `tui-perf-load` design 吸收并随归档晋升正式 ADR）。
   - 耗时日志 MUST NOT 含明文/密钥材料，语义与操作审计分离（写入 `perf.log`，不混入 `audit.log`）。
   - net 与 load 相互独立、可并行 apply（文件范围不重叠：net 动 cmd/provider.go、cmd/tui.go 构造段、history tab；load 动 storage 读路径、internal/tui 数据装载/同步刷新）。

## 数据流（子 change 依赖）

```
tui-perf-log ──▶ tui-perf-net（网络埋点接入）
             └──▶ tui-perf-load（加载/扫描埋点接入与回测）
tui-perf-net 与 tui-perf-load 相互独立
```

## 错误处理策略

driver 层无运行时错误面；跨子 change 只约束语义：耗时日志写入失败静默降级（不影响业务路径，与审计同策略）；网络连接复用失败退回现行为（每动作新建连接）；快照批量读失败回退逐文件读语义。

## Risks / Trade-offs

- [ServerProvider 多 goroutine 并发复用可能暴露非线程安全状态] → net 子 change 做并发审计与 `-race` 测试，不安全处加锁后再单例化
- [manifest 进程内缓存遇跨进程 rekey 可能过期] → load 子 change 以 metadata.json 代际（stat）为缓存失效界，锁内清算语义不变
- [3 个子 change 串行 apply 中途状态不一致] → 每个子 change 完成即 `validate --strict` 且保持可构建，driver 对应 checkbox 才勾选

## Migration Plan

无数据迁移。实施全部完成后按协议归档：先归档全部子 change（spec 应用到 `openspec/specs/`），再归档 driver。

## Open Questions

无
