# Design: tui-perf-load

## Context

grill 根因 R2/R3：519 个密文文件 × 2~3 趟 × 每文件「EnsurePrivateDir + flock + recoverRekey（重读 manifest）+ 开/关 config root」；读路径全部经 `withVaultRead` → `WithVaultMutation`（storage/mutation.go:52-66）拿排他锁，各 Tab 并行加载被串行化；`loadRekeyManifest` 每次重开 config root 读文件（rekey_manifest.go:249-273）。同步状态 `LocalSyncSnapshot` → collect 全量读取全部密文（internal/provider/server_state.go）。`reloadAllTabs` 触发的 `Reload()` 置 `loaded=false`（env_tab.go:126-129），UI 清回占位。

## Goals / Non-Goals

**Goals:** 单趟全量本地读 ≤300ms（当前 ~1-1.5s）；pull 后列表不清空；同步状态无变更时刷新 stat 级

**Non-Goals:** 改加密格式/文件布局（per-variable `.enc` 保持）；改 rekey 事务流程本身；单条读写路径语义变化；跨进程共享缓存（缓存只在进程内）

## Decisions

1. **批量读 API 在 storage 层**：新增 `LoadVaultSnapshot(key)` 类批量入口——单次 `WithVaultMutation` 锁内完成清算 + 一次性读全部条目并解密，返回按域组织的只读快照；既有逐文件 API 保留并基于同一清算路径（单条 `senv env get` 不变慢也不变语义）。
2. **manifest 进程内缓存，以 metadata.json 代际为失效界**：缓存放 Manager（进程内）；每次锁内清算前 stat metadata.json（mtime+size），代际变化即重载 manifest；本进程写/恢复动作完成后主动失效。锁语义不变——这是把「每读一次的清算税」合并为「每代际一次」，不是绕过清算。**ADR：** grill 候选 `adr-读路径锁语义`（难逆转：读路径并发正确性根基；后人费解：为何读也要过排他锁与清算、缓存为何以代际为界；真实取舍：逐文件强清算 vs 代际校验+批量的性能差一个量级）——本 change 落地时在 `docs/adr/` 落盘正式 ADR。
3. **快照消费方收敛**：TUI 侧建 snapshot registry（持锁生成、原子替换）：env Tab 装载不再 `ListGroups`+`List` 双趟；全局搜索、deref 视图、AI Tab 凭据引用收集全部读快照；写消息到达 → registry 失效 → 后台单趟重建 → 原子替换。跨 Tab 重复触发重建时合并为一次（single-flight）。
4. **SWR reload 两阶段**：`Reload()` 改为「标记 stale + 触发后台重建」，替换前不清 `loaded`、不渲染占位；`envLoadedMsg` 等装载完成消息保留既有 cursor/过滤状态回填逻辑。路由修复顺带：装载完成消息广播到所有 Tab（修掉 model.go:354-355 只转发激活 Tab 导致的后台装载丢弃），否则切 Tab 会触发重复加载。
5. **同步状态增量收集**：collect 以「上次快照 + 条目 stat（mtime/size）」做差分；vault 无写入（进程内写计数 + stat 目录代际）时零解密；AutoPush 的 dirty 判定与冲突检测输入不变。与 `tui-perf-net` 的连接复用无耦合，可独立回测。
6. **验收回测**：以 `tui-perf-log` 埋点为准——单趟装载阶段行 ≤300ms、启动到列表可用 ≤1.5s（配合 net 子 change）、SWR 场景列表可见性人工核对。

## Risks / Trade-offs

- [跨进程 rekey 后 stale manifest 导致解密失败] → 代际 stat 每次清算前校验，代际变化重载；解密失败路径已有明确报错，不静默错读
- [snapshot 内存占用] → 当前 vault 全量明文 <2.5MB，进程内常驻可接受；超大 vault 后续再做分域懒加载（不在本次）
- [stat 级差分漏判外部直接改文件] → 与现状「写计数」同风险面；collect 在 push 前仍走全量校验路径的既有语义不变（dirty 判定以增量预筛 + push 时 server 端乐观锁兜底）
- [SWR 期间用户操作基于旧数据] → 写操作走既有「校验后落盘」路径，stale 只影响展示不影响写入正确性

## Migration Plan

无数据迁移；无格式变更。ADR 随本 change 落盘 `docs/adr/`。

## Open Questions

无
