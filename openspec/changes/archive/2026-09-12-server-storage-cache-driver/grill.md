# Grill：server-storage-cache

## 决策记录

| # | 决策 | 结论 | 理由 | 状态 |
|---|------|------|------|------|
| D1 | 「统一存储层」的含义 | 抽接口 + 收拢生命周期两者都做；migrate 保持 raw `*pgx.Conn`（本就该在池之前跑） | 抽接口是缓存 decorator 的前提；收拢消除 `main.go` 双 store 实例隐患；分开做要动两遍 | settled |
| D2 | 缓存对象 | auth 结果 + vault 查找 + vault seq；不缓存 entries 密文 / metadata / access log。实现期修正：name→vaultID 在 decorator 接缝不可获得（Store 接口不返回 vaultID），收敛为 seq 快捷判定（一次省 3 次往返，见 auth design 决策 8） | auth 每请求 1 次查询是最大头且表读极重写极稀；vault 查找/seq 廉价高命中；entries 体积大有内存险且增量 pull 已有索引；access log 只写不读 | settled |
| D3 | 失效语义（初版，已被 D8 修正） | 曾推荐「同进程写路径同步失效」 | **前提错误**：block/revoke 走 `senv-server admin` 独立一次性进程（无 HTTP admin 端点，已验证路由表），serve 进程收不到同进程失效信号 | superseded by D8 |
| D4 | access log 同步写 | 本轮不动 | 它是写路径，缓存帮不了；异步化引入丢日志窗口，与主题正交，另开 change | settled |
| D5 | TouchClient 每请求往返 | 内存化节流：last_seen 记内存、至多 1 分钟 flush，停机 best-effort flush | last_seen 纯展示字段无安全语义，崩溃丢几分钟无实害；白捡每请求 1 次往返；flush 周期与现有 SQL 节流粒度对齐 | settled |
| D6 | 多实例姿态 | 单实例内存态 + 文档声明，与 rate limiter 先例一致；多副本时缓存与限速器一起重新设计 | 部署实况是单 docker 单机；为不存在的多副本付复杂度违背「避免过度设计」 | settled |
| D7 | 性能验证 | auth+pull 热路径 Go benchmark（testcontainers PG），before/after 进验证记录；不加 server 侧 p95/耗时日志 | benchmark 是最小可信证据；p95 牵涉新服务端日志术语（会碰 CONTEXT.md avoid 约定） | settled |
| D8 | auth 缓存失效机制（D3 重开后） | pg LISTEN/NOTIFY：serve 持专用连接监听，admin 路径 revoke/block 时 `pg_notify` 广播，serve 收到**全清 auth 缓存**；30s TTL 仅作 NOTIFY 丢失兜底；监听重连也全清 | 「屏蔽即刻失效」是安全承诺，不能用改措辞迁就实现；NOTIFY 广播全清实现量小且管理操作极 rare；与 Q6 拒掉的多副本 LISTEN/NOTIFY 不同——这是单实例**多进程**通信 | settled |
| D9 | 接口与缓存形态 | `store` 包内定义 `Store` 接口，具体类型改名 `pgStore`、构造器 `NewSQL(pool)`；缓存为同包 decorator `WithCache(inner)`；`handler.New` 吃接口；admin 一次性进程无缓存 | 单一接口一处定义；decorator 可用假 Store 独立单测；handler 包窄接口 + cache 包宽接口是两套接口的脆性；埋进 pgStore 则无法 mock | settled |
| D10 | vault/seq 缓存参数 | `vaultName → (vaultID, seq)`；vault 查找无失效（已验证 vault 只增不删）；seq 由 push 同进程更新，pull 侧偏旧只会多跑一次范围查询（方向性安全）；TTL 5min + cap 4096 | 数字只为防病态膨胀，不是性能旋钮；seq 偏旧=值更小=不满足「已最新」快捷判断=落回 DB 查询，无假阳性 | settled |
| D11 | singleflight 防击穿 | 不加；朴素 `sync.Mutex + map`，benchmark 显示锁竞争再补 | 个人工具并发量级到不了击穿场景（client 端还有 2s pull 节流）；避免过度设计 | settled |
| D12 | ADR 候选 | 一篇：`server-cache-out-of-band-invalidation`（单实例 server 为何用 NOTIFY——因为 admin 是独立进程）；接口化与单实例姿态只进 design.md | 三门槛全中：难逆转（失效机制定型后换伤筋动骨）、后人费解（不看 main.go 不知道 admin 进程外）、真实取舍（TTL-only vs NOTIFY vs 不缓存 auth） | settled |
| D13 | driver 命名与切片 | task `server-storage-cache`；切片：`server-storage-cache-unify`（纯重构）/ `server-storage-cache-auth`（缓存+NOTIFY+benchmark）/ `server-storage-cache-touch`（TouchClient 节流+ADR+文档）；CONTEXT.md 无需改动 | 切片 1 零行为可单独回滚；切片 2 安全敏感面独立 review；切片 3 独立小优化 | settled |

## 术语表

| 术语 | 本任务语境下的定义 | 与既有用词的关系 |
|------|--------------------|------------------|
| （无新增） | 缓存为实现细节不进 glossary；「屏蔽（即刻失效）」「吊销（不可逆）」语义经 D8 保持不变，最坏 30s 兜底窗口记录于 ADR 与 proposal 安全性分析，不改措辞 | 沿用既有定义 |

## ADR 候选

<!-- 仅记录满足「难逆转 + 后人费解 + 真实取舍」三门槛的；由 design.md 吸收或随 change 归档晋升 -->

- [x] adr-server-cache-out-of-band-invalidation: auth 缓存因管理操作（revoke/block）在独立一次性进程执行而采用 pg LISTEN/NOTIFY 广播全清 + 30s TTL 兜底；单实例内存态姿态与 rate limiter 一致（出处：D6/D8/D12）→ 已记入 `docs/adr/0018-server-cache-out-of-band-invalidation.md`；`cross-machine-ai-mcp-sync-driver` 的候选落地时顺延取 0019

## 未决问题

无
