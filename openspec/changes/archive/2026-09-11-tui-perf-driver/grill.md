# Grill：tui-perf

## 根因结论（2026-09-11 实测，已向用户汇报）

体感「senv tui 加载变量 ~5s」由三段串联构成，均有测量证据：

1. **R1 网络建连慢且连接不复用**：到 senv.wii.pub 的 HTTPS，TCP connect 仅 3ms，但 TLS+首请求 0.35~1.5s（curl 实测，含 keep-alive 对照：同连接第 2/3 个请求仅 0.10s）。`getSyncProvider()`（cmd/provider.go:19）无进程内 memo，每次调用 new 新 provider（=新 http client）：TUI 启动时 History Tab（cmd/tui.go:211-229）与 pullSync（cmd/tui.go:115-121）各建独立实例，各付一次握手；CLI 每条命令 autoPull（cmd/autosync.go:63）同理。
2. **R2 全量重复读 + 排他锁串行**：vault 共 519 个密文文件（envs 268 / texts 118 / hosts 63 / keypairs 25 / 根 45），每读一个文件都付「EnsurePrivateDir + flock 排他锁 + recoverRekey→重读 manifest + 开/关 config root」的固定税（storage/mutation.go:24-66、mutation_lock_unix.go:18-56、rekey_manifest.go:249-273）。envTab.load 一趟读两遍（env_tab.go:137-143 ListGroups+List），AI Tab 启动第三遍（ai_tab.go:187-217），各 Tab 并行加载被排他锁近似串行。实测单进程全量 CLI 路径暖启动 1.2~2.6s，其中 CPU 仅 0.03s（2%）——纯 IO/锁开销，非解密/JSON。
3. **R3 reload 级联清空 UI**：pull 落地且 Applied>0 时 reloadAllTabs（model.go:269-274）→ envTab.Reload 置 loaded=false（env_tab.go:126-129），列表清回「加载环境变量中…」占位，再全量读两遍。启动时序 ≈ 本地首渲（1~1.5s）→ pull（TLS+请求 1~2s）→ 全量重载（1~1.5s）≈ 4~5s，与体感吻合。

排除项：PBKDF2 600k（有会话缓存 /run/user/1000/senv 时不触发，且 CPU 实测极低）；磁盘（本地 NVMe ext4，非 NFS）；models-dev.json 8.4MB（仅 AI 表单保存时用）；解密/JSON 解析（CPU 占比可忽略）。Audit Tab 启动全量读 4289 行（892KB）为次要项。

## 决策记录

| # | 决策 | 结论 | 理由 | 状态 |
|---|------|------|------|------|
| D1 | 优化范围 | 聚焦暖启动 TUI 变量列表加载（~5s）；冷启动 PBKDF2 不动 | 用户实测痛点即此；PBKDF2 属安全设计 | settled |
| D2 | 埋点与优化先后 | 根因已用直接测量定位（CLI 计时 + curl 分解 + 代码走读）；耗时日志保留为交付物之二，服务验收与回归 | 用户要求找根因，已找到；不再依赖日志探根因 | settled |
| D3 | 耗时日志形态与落点 | (a) 独立 `~/.log/senv/perf.log`，log/slog JSON lines，超阈值才记（默认 100ms，env 可调/可关）；记录启动各阶段、单次全量加载、网络请求（含连接复用）、同步扫描，附规模维度；术语定名「耗时日志」 | TUI 也能写、零干扰；与操作审计语义分离；slog 不引新依赖 | settled |
| D4 | 规模与环境 | 10 个 active group、519 密文文件、本地 NVMe ext4、server 模式（senv.wii.pub，TLS 建连 0.35~1.5s、复用连接 0.1s）、会话缓存通常在效 | 实测 | settled |
| D5 | 加载去重必做 | 必要但不足够，并入 R2 修复包统一决策 | 双读/三读是确定性浪费，但单独做消不掉 5s | settled（方向） |
| D6 | 修复范围 | A 网络（provider 单例/连接复用、History 延迟加载）+ B 读路径（单趟共享快照、锁与 manifest 税批量化）必做；C 服务端 TLS 慢排查拆独立调查，不阻塞本次 | A+B 是 5s 主体且纯 client 侧可解；C 属 infra | settled |
| D7 | reload 语义 | stale-while-revalidate：pull 应用变更后保留旧列表可操作，后台刷新完成后静默替换 | 消掉「二次清空」体感 | settled |
| D8 | 验收目标 | ① 暖启动到 env 列表可用 ≤1.5s，远端有变更时 ≤2.5s 内静默稳定（期间旧列表可用）；② 单趟全量本地读 ≤300ms；③ 进程内网络建连 ≤1 次；④ 耗时日志可分解启动各阶段占比 | 用户按推荐确认 | settled |

## 术语表

| 术语 | 本任务语境下的定义 | 与既有用词的关系 |
|------|--------------------|------------------|
| 耗时日志（Perf Log） | 记录关键路径耗时（花了多久）的性能日志，按阈值过滤；已同步到 CONTEXT.md | 与「操作审计（Operation Audit）」（记做了什么）刻意区分，不混写 |

## ADR 候选

- [ ] adr-读路径锁语义：若采纳「读路径免每读 flock / manifest 进程内缓存」，涉及存储并发正确性与 rekey 安全取舍，难逆转、后人费解、真实取舍三门槛齐备（出处：R2/D6-B）

## 未决问题

无（frontier 已清空，用户于 2026-09-11 确认按推荐收敛）。
