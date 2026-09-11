# tui-perf-load

## Why

vault 共 519 个密文文件，每个文件读取都要付「flock 排他锁 + rekey 清算 + 重读 manifest + 开/关 config root」固定税，而 TUI 启动要把全 vault 读 2~3 遍（env Tab 的 `ListGroups`+`List` 双趟、AI Tab 凭据引用收集第三趟），各 Tab 并行加载还被同一把排他锁串行化——实测单趟 1~1.5s、CPU 仅 0.03s，纯 IO/锁开销。且 pull 应用变更后 `reloadAllTabs` 把列表清回「加载环境变量中…」占位再全量重读，用户看到的是「二次清空」。grill 决策 D5/D6-B/D7/D8①②：单趟快照多消费方共享、锁税批量化、reload 改 stale-while-revalidate、同步状态增量收集。

## What Changes

- vault 数据单趟加载：一次全量遍历构建内存快照，env Tab、全局搜索、AI Tab 凭据引用收集等消费方复用同一快照；写操作后失效并单趟重建
- 读路径批量化：批量读取在单次锁获取内完成；rekey manifest 进程内缓存（以 metadata.json 代际为失效界），消除每文件重复清算
- 同步状态快照（`LocalSyncSnapshot`/collect）增量收集：无变更时只比对待推送标记/stat，不全量读取解密全部密文
- reload 语义改 stale-while-revalidate：pull 应用变更后保留旧列表可操作，后台单趟重载完成后静默替换（保留光标/过滤状态）
- 读路径优化 MUST NOT 削弱 rekey 混合密钥代际隔离（grill ADR 候选 adr-读路径锁语义随本 change 落地并晋升正式 ADR）

## Capabilities

### New Capabilities

（无）

### Modified Capabilities
- `tui-viewer`: env 数据单趟加载与共享快照；后台拉取应用变更后不得清空展示（stale-while-revalidate 静默替换）
- `rekey-recovery`: 读路径批量清算与 manifest 进程内缓存（隔离语义不削弱）
- `server-sync`: 本地同步状态快照增量收集

## Impact

- `internal/storage`（批量读 API、manifest 缓存与失效）、`internal/env`（ListGroups/List 合并为单趟遍历）、`internal/tui`（快照消费方：env Tab、搜索、deref、AI Tab；Reload 两阶段）、`internal/provider/server_state.go`/`server_auto.go`（增量 collect）
- 兼容：逐文件读 API 保留（单条 get 路径不变）；跨进程并发与 rekey 正确性由既有锁语义保障
- 验收：单趟全量本地读 ≤300ms（D8②）、暖启动列表可用 ≤1.5s（D8①，配合 `tui-perf-net`）
