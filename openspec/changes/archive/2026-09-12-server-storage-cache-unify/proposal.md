## Why

senv-server 的 DB 访问没有统一出口：`handler` 依赖具体类型 `*store.Store`（无接口，handler 测试只能起 testcontainers 真 PG）；`main.go` 的 `pruneAccessLogsPeriodically` 自建第二个 store 实例、admin 子命令每次 `withStore` 新建连接池。这是后续缓存改造（`server-storage-cache-auth`）的前置重构。

## What Changes

- `internal/server/store` 定义 `Store` 接口；现具体类型改名 `pgStore`，构造器改 `NewSQL(pool)`，`store.New` 保留为返回接口的兼容入口
- `handler.New` 与 `handler.Server.store` 改吃 `Store` 接口；handler 测试改用假 Store 覆盖 auth/metadata/pull/push/history 路径，不再强制 testcontainers
- 生命周期收拢：serve 进程单一 store 实例（prune goroutine 复用同一实例）；admin 子命令统一经同一构造路径（保持无缓存、一次性进程）；`runServe` 启动前 schema 预检行为不变
- 纯重构：无任何可观察行为变化，现有 store/handler 测试断言不改（允许改构造方式）

## Non-goals

- 不引入任何缓存（属 `server-storage-cache-auth`）
- 不动 `TouchClient` 节流（属 `server-storage-cache-touch`）
- 不动 migrate 的 raw `*pgx.Conn`（本就该在连接池之前运行）
- 不改任何 SQL 语义、API 行为与零知识不变式

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

（无——纯重构，`.openspec.yaml` 已设 `skip_specs: true`）

## Impact

- `internal/server/store/`（接口抽取与改名）、`internal/server/handler/`（依赖类型改接口 + 测试改造）、`senv-server/main.go`（生命周期收拢）
- 后续切片 `server-storage-cache-auth` / `server-storage-cache-touch` 依赖本切片的接口缝
