## Context

现状（driver `server-storage-cache-driver` grill D1/D9）：`internal/server/store` 全部为具体类型 `*store.Store`（`store.go:101-117`，pool + historyRetain 两字段），`handler.New(st *store.Store)` 直接依赖具体类型（`handler.go:51,60`），handler 测试只能靠 `internal/server/testdb`（testcontainers 真 PG，无 docker 即跳过）。DB 访问路径分散：`main.go:148` prune goroutine `store.New(pool)` 自建第二实例；`main.go:456-467` `withStore` 每 admin 子命令新建池；`main.go:118` serve 启动另开一次 `pgx.Connect` 做 schema 预检（预检语义保留）。

## Goals / Non-Goals

- Goals：handler 面向接口；serve 进程单一 store 实例；为 auth 切片留出 decorator 接缝（`WithCache(inner Store) Store`）
- Non-Goals：缓存、节流、SQL 语义改动；migrate 的 raw `*pgx.Conn`（先于池运行，属设计而非散乱）

## Decisions

1. **接口定义在 store 包**（而非 handler 消费侧）：decorator 与 handler 需共享同一接口；单实现单消费者场景下两处定义只增脆性。
2. **具体类型改名 `pgStore`、构造器 `NewSQL(pool)`，`store.New` 保留**：`New` 返回接口作为兼容入口，main.go 与测试逐步迁移；避免一次性全仓改名。
3. **`Pool()` 逃生舱保留在接口上**（`store.go:117` 注释：admin/migrate 入口使用）：prune goroutine 收拢后仍需 pool 做 `pgxpool` 级批量删除？否——`PruneAccessLogs` 已在 store 内，收拢后 prune goroutine 复用同一 store 实例，不再接触 pool；`Pool()` 仅 serve 启动预检与 migrate 路径使用。→ 接口含 `Pool()` 但标注"启动期使用，请求路径禁止"。
4. **handler 假 Store 测试**：新增 `internal/server/handler/fakestore_test.go`，按接口实现内存版（map 存储）；仅覆盖 auth/metadata/pull/push/history 的请求-响应断言，不复制 SQL 行为细节（事务/冲突语义仍由 store 包 testcontainers 测试守住）。

## Risks / Trade-offs

- [接口面过大（~25 方法），后续每加 store 方法要同步接口] → 接受：单包内变更，编译器强制；拆分域接口属过度设计
- [假 Store 与真 pgStore 行为漂移] → 事务/冲突/revision 语义只由 store 集成测试断言，handler 假 Store 只测 HTTP 映射；漂移面收敛到状态码与 JSON 形状
- [改名撞历史 diff blame] → 一次切片完成，git blame 以 rename-diff 可追溯

## Migration Plan

单仓单切片，编译期保证：接口抽取后 `handler` 编译不过即未完成。回滚 = revert 本切片提交，无数据/部署面变化。

## Open Questions

无
