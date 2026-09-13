## Context

首屏慢的结构（详见 explore 结论，证据行号以实现时代码为准）：

```
Init() ── tea.Batch(全 8 tab load + refreshSync + pullSync)
   │  每个 load: withVaultRead → flock(LOCK_EX)  ← 读读互斥
   │  pull:      在 flock 内做 api.Pull（网络，2s budget）
   └─ 全部串行排队 → 首屏 = 排到自己那一刻
```

关键事实：
- `internal/tui/snapshot.go` 是进程内 single-flight memo，冷启动第一趟仍需全量解密，对首屏零加速。
- 仓库不存在磁盘明文/加密快照缓存；「local data renders first」只兑现了「pull 不阻塞渲染循环」一半。
- `model.go` 注释声称 tab 懒加载，实际 Init 全量加载；仅 History Tab 真懒加载（`visited` 模式可复用）。
- text 域是逐条目 `withVaultRead`（N 次排它锁），env 已是单趟 `Snapshot()`。

## Goals / Non-Goals

**Goals:**
- A：pull 网络阶段与 vault 锁解耦；text 读取单趟化。
- B：Init 只加载当前 tab + 共享快照，其余 tab 首次聚焦加载。
- C：加密快照缓存让冷启动首屏在快照解密时限内可见，真实解密完成后以真相替换。
- 保持 perflog 埋点可验证各项预算。

**Non-Goals:**
- 不改 sync throttle 窗口、`--refresh` 语义、push 路径。
- 不处理无 session 时逐次 PBKDF2-600k 的密码路径（独立变更，C 的快照对该路径同样生效可缓解）。
- 不改 UI 布局、键位、alt-screen 行为。

## Decisions

### D1. pull 网络移出锁外（三段式）

`internal/provider/server.go` 的 pull 拆为：
1. **读锁**：收集本地 manifest（版本/指纹），构造请求；
2. **无锁**：`api.Pull` 网络请求（可与其他 tab 读并行）；
3. **写锁**：应用响应（写密文落盘）。

替代方案「保留单锁、缩短网络 budget」——网络不可控，budget 只是上限，锁排队问题依旧。替代方案「读锁降级为共享锁」——unix flock 无共享语义（`LOCK_SH` 需全调用方配合改造），收益小于三段式且风险更高。

**并发正确性**：原实现在 pull 全程持排它锁，天然挡住了 pull 期间的本地写。拆锁后本地写可插入网络阶段；pull 语义本就是「服务端状态应用本地」，本地新写入仍走 dirty 队列由 push 上送（现有 autosync 模型），与 pull 应用无冲突。应用阶段持写锁，落盘原子性不变。

### D2. text 域单趟快照

`internal/text` 新增 `Snapshot()`：一次 `withVaultRead` 内完成全部组的密文读取 + 解密，返回聚合结构；TUI text tab 改走 `snapshot.go` 的进程内 memo（与 env 同等待遇），写操作/pull 应用后 `Invalidate()`。`List`/`ListGroups` 保留给其他调用方。

替代方案「给 withVaultRead 加共享锁」——同 D1，放弃。

### D3. 真懒加载复用 History 的 visited 模式

`model.go` Init 仅加载当前聚焦 tab；tab 切换处（现有 `visited` 置位逻辑所在）对未加载 tab 触发一次性加载。全局搜索是跨域的：搜索若依赖未加载域，先异步补齐该域快照再出结果（显示加载态），不为了搜索而 Init 全量加载。

替代方案「保留全量加载但并发优先级排队」——锁竞争未除，排队本身即用户感知的慢。替代方案「骨架屏 + 全量加载」——只改善观感不改善时延，作为 B 的附带自然结果而非独立方案。

### D4. 加密快照缓存：明文不落盘

新增快照文件（建议 `<dataPath>/tui-snapshot.enc`）：
- 内容：全域明文列表（env/text/config/ssh 的展示所需字段）序列化后 **用 vault 主密钥 AES-256-GCM 加密**——密钥与条目加密同源，不引入新的密钥管理。
- 指纹：vault salt + 密文清单聚合（路径+size+mtime），用于粗判 staleness；精判在后台真实解密完成后逐域比对，不一致即替换展示。
- 写入时机：TUI 正常退出时 + 写操作/pull 应用后（best-effort，失败静默）。
- 读取时机：`tui.New` 后首个 load 之前；解密失败/指纹不符/文件缺失 → 静默回退原路径。
- 权限：目录 0700、文件 0600，与 vault 一致。

替代方案「明文快照」——违反项目安全基线，不可接受。替代方案「快照存进程外/共享内存」——复杂度不抵收益。替代方案「不缓存、只优化解密」（如并发解密条目）——仍受 PBKDF2/IO 下限约束，做不到「首屏即时」，作为 C 的补充优化可后续考虑。

## 安全性分析

- 快照文件静态安全级别 = vault 本身：同密钥 AES-256-GCM、同权限位；攻击者能读快照密文即已能读 vault 密文，不扩大攻击面。
- 明文仅存在于内存，与现有 TUI 运行态一致；退出时由 GC 持有，不落盘。
- 指纹不含明文/密钥材料，只含元数据（路径、size、mtime），泄露不扩大信息量。
- 回退路径保证快照损坏时行为退化为现状，不产生错误数据展示。

## Risks / Trade-offs

- **pull 拆锁引入新交错路径**（pull 期间本地写）→ pull 应用保持写锁原子写；dirty 队列语义不变；补并发测试覆盖「pull 中写入」。
- **懒加载改变集成测试假设**（现有测试多假设 Init 后全 tab 就绪）→ 测试中统一走「切到目标 tab」的前置；列为 tasks 显式步骤。
- **快照短暂过期窗口**（启动展示后后台发现不一致才替换）→ 与现状 pull 后台更新的窗口同级，可接受；写操作后立即失效并同步重写。
- **指纹用 mtime 可被触碰干扰** → 只是粗判，精判靠解密后比对；最坏情况 = 多一次回退，无副作用。
- **tui.managers 无细分埋点** → 本次在 pull 三段各加 perflog，验证拆锁收益可测。

## Migration Plan

1. A（D1+D2）先行，独立可发布，perflog 验证；
2. B 随后，行为可见，需更新相关测试；
3. C 最后，带功能开关（如 `SENV_TUI_SNAPSHOT=off` 可回退），验证稳定后默认开启。
每步独立通过 `make check`，无数据迁移。
