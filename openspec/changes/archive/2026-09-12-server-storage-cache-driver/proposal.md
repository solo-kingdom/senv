## Why

senv-server 的 DB 访问没有统一出口：`handler` 依赖具体类型 `*store.Store`（无接口，handler 测试只能起 testcontainers 真 PG）；`main.go` 里 `pruneAccessLogsPeriodically` 自建第二个 store 实例、admin 子命令每次 `withStore` 新建连接池。同时服务端**零缓存**，每个请求固定 3 次 DB 往返（`AuthenticateWithClient` 查询 + `TouchClient` UPDATE 往返 + access log 同步 INSERT），Pull 再加 3 次查询。其中 auth 查询是每请求最大头的固定开销，而 tokens/clients 是读极重、写极稀（仅签发/吊销/屏蔽）的表——非常适合内存缓存。本 driver 把存储层统一为接口 + 单一实例，并对 auth 与 vault 查找/seq 引入进程内缓存。

## What Changes

- 本 change 是 taskflow driver，不直接改代码，只编排子 change
- `store`包定义 `Store` 接口，现具体类型改名 `pgStore`、构造器 `NewSQL(pool)`；`handler.New` 改吃接口，handler 测试可用假 Store mock
- 缓存为同包 decorator：`store/cache.go` 的 `WithCache(inner Store) Store`，缓存三类数据——auth 结果（token_hash → AuthResult）、vault 查找（vaultName → vaultID）、vault seq
- auth 缓存失效：serve 进程持专用连接 LISTEN 失效通道，admin 路径 revoke/block 时 `pg_notify` 广播，serve 收到即**清空全部 auth 缊存**；30s TTL 仅作 NOTIFY 丢失/监听瞬断的兜底；每次监听重连也全清
- vault 查找缓存无需失效（vault 只增不删）；seq 由 push 同进程更新，pull 侧缓存偏旧只会多跑一次范围查询（方向性安全）；TTL 5min + 容量上限 4096
- `TouchClient` 内存化节流：last_seen 记内存、至多 1 分钟 flush 一次，停机时 best-effort flush；省掉每请求一次 UPDATE 往返
- main.go 生命周期收拢：serve 进程单一 store 实例（prune goroutine 复用）；admin 子命令统一构造路径（保持无缓存）；`runServe` 的 schema 预检行为不变；migrate 保持 raw `*pgx.Conn` 不动
- 性能验证：auth+pull 热路径 Go benchmark，before/after 数字记入 driver 验证记录
- ADR 候选 `server-cache-out-of-band-invalidation` 落盘（编号按落地时 `docs/adr/` 扫描取下一空位）；docs/senv-server.md 补单实例姿态说明

## Non-goals

- 不做多副本/横向扩展支持：缓存与 rate limiter 同为"单实例内存态"姿态，多副本时两者一起重新设计（LISTEN/NOTIFY 广播或多级 TTL）
- 不缓存 entries 密文、vault metadata、access log 读路径
- 不动 access log 同步写：异步化是独立取舍（丢日志窗口），另开 change
- 不加 singleflight/击穿防护：并发量级到不了，benchmark 显示锁竞争再补
- 不加 server 侧 p95/耗时日志：perflog 保持 client-only，不引入新的服务端日志术语
- 不改同步协议、API 语义与零知识不变式：缓存只存 token 哈希派生键、userID/clientID、状态枚举、vault 名/id/seq 等非明文元数据
- 不动 migrate 的 raw `*pgx.Conn` 用法（本就该在连接池之前运行）
- 不改 CONTEXT.md：「屏蔽即刻失效」语义保持，缓存是实现细节不进 glossary

## 安全性分析

- **缓存内容无新增敏感面**：key 为 token 的 SHA-256 哈希（server 本就只存哈希），值为 userID/clientID/状态枚举/vault id/seq——全是 server 侧既有元数据，进程内存驻留，不落盘、不出进程
- **失效语义**：正常路径下 revoke/block 经 NOTIFY 即刻生效（与 CONTEXT.md「屏蔽即刻失效」「吊销不可逆作废」承诺一致）；最坏窗口 = NOTIFY 丢失（监听连接瞬断）且 TTL 未到，此时已屏蔽 client 至多还能通过认证 30s（TTL 兜底上限）；监听重连时全清缓存进一步压缩窗口
- **诚实边界**：30s 最坏窗口是 (a) 方案相对同进程失效的已知代价，已在 ADR 与 design 中显式记录；若不可接受，退路是放弃 auth 缓存（决策 D8 备选 c）
- admin 一次性进程不装缓存，无失效歧义；注册新 client/token 走 serve 进程写路径，新 token 天然 cache miss

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 会修改，实施前切任务分支 |

## 验收标准

- [x] `store` 包定义 `Store` 接口；`handler.New` 改吃接口；handler 测试可用假 Store 覆盖 auth/metadata/pull/push 路径，不再强制 testcontainers
- [x] 生命周期收拢：serve 进程单一 store 实例（prune goroutine 复用同一实例）；admin 子命令统一构造路径；`runServe` schema 预检行为不变；纯重构切片现有 store/handler 测试全绿且不改断言（允许改构造方式）
- [x] auth 缓存命中时该请求零 auth DB 查询；`RegisterClient` 新 token 首次认证走 DB 后可命中
- [x] NOTIFY 失效链路：admin revoke/block 后 serve 进程 auth 缓存清空，已缓存 token 的下一个请求回库并被拒（测试覆盖 revoke 后 ≤1 请求内失效）
- [x] 监听连接断开自动重连；重连时全清缓存；短 TTL 注入测试验证兜底生效
- [x] vault/seq 缓存：客户端已最新时 pull 命中缓存；push 后同进程 seq 即时更新，不产生假「已最新」
- [x] TouchClient 内存节流：last_seen 至多 1 分钟 flush 一次，停机 best-effort flush；admin list-clients 显示值延迟 ≤1 分钟
- [x] benchmark：auth+pull 热路径 before/after 数字记入 driver 验证记录（testcontainers PG）
- [x] ADR 落盘 + `docs/senv-server.md` 单实例姿态补句；`.agents/skills/senv-cli/SKILL.md` 如有用户可见变化则同步；`make check` 通过

## Driver 协议

- 本 change 无 spec 增量（`.openspec.yaml` 已设 `skip_specs: true`）
- 子 change 一律命名 `{task}-<slice>`，与本 change 同一 planning root；跨 root 时在涉及面表显式记录 root 或 store id
- 实现进度只认子 change 自己的 `tasks.md`；本文件的 checkbox 只在对应子 change 全勾且 `validate --strict` 通过后才勾
- 涉及面里角色为 `必须` 的仓在实施前切任务分支：没有则 `git switch -c`，已有则 `git switch`。不许 stash / reset / 强制切换。工作树 dirty 时：未提交路径仅含当前 task 的 OpenSpec change（`openspec/changes/{task}-*`）则直接切；否则列出路径并确认是否继续 checkout。用户不同意、git 拒绝或切错仓时停下
- 只有「checkbox 全勾」「需要用户决策」「本轮预算耗尽」三种情况允许结束一轮；单项做不了就保持未勾，在验证记录写一行原因后继续下一项
- 结束时逐条列出未勾项与原因，不按 change 汇总

## 验证记录

- 2026-09-12 grill：13 项决策 settled（D3 在第二轮因「admin 进程外执行」新事实重开并修正为 D8，见 `grill.md`），1 项 ADR 候选 `server-cache-out-of-band-invalidation` 待 propose 阶段晋升。
- 2026-09-12 子 change `server-storage-cache-unify` 落地：tasks 8/8 勾选、`validate --strict` 通过。`Store` 接口 + `pgStore`/`NewSQL` 落地，handler 面向接口并新增 `fakestore_test.go`（9 个 fake 测试，无 docker 可跑），serve 进程单一 store 实例、admin 统一 `NewSQL`；`make check` 全绿，store/handler testcontainers 集成测试在真 Postgres 上通过，测试 diff 仅签名/构造改动、零断言修改。
- 2026-09-12 子 change `server-storage-cache-auth` 落地：tasks 9/9 勾选、`validate --strict` 通过。decorator 缓存 + pg_notify 广播失效 + 专用连接 LISTEN 监听（断线重连全清）落地；集成测试复刻双进程拓扑（revoke→401、block→403、unblock→恢复、pg_terminate_backend 断连后重连全清）。基准（本机 docker PG，`-benchmem`）：auth 未命中 37,909 ns/op、17 allocs → 命中 119.6 ns/op、1 alloc（≈317×）；已最新 pull 未缓存 103,314 ns/op、27 allocs → 快捷判定 45.3 ns/op、0 alloc（≈2,281×）。设计修正：grill D2 的「vault 查找缓存」在 decorator 接缝不可实现（接口不暴露 vaultID），收敛为 seq 快捷判定（auth design 决策 8）；`make check` 全绿。
- 2026-09-12 子 change `server-storage-cache-touch` 落地：tasks 6/6 勾选、`validate --strict` 通过。TouchClient 内存节流（decorator 内 set 缓冲 + 1min 周期 flusher + 停机 5s best-effort flush，SQL 谓词保留为写侧双保险）、ADR-0018 落盘、`docs/senv-server.md` 单实例约束补句。无用户可见 CLI 变化，`senv-cli/SKILL.md` 无需更新；`make check` 全绿。driver 验收标准 10/10 勾选。
