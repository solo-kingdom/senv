## 1. 接口抽取

- [x] 1.1 `internal/server/store`：定义 `Store` 接口（现 `*Store` 全部公开方法 + `Close` + `Pool()`，`Pool()` 注释标注"启动期使用，请求路径禁止"）；现具体类型改名 `pgStore`，构造器 `NewSQL(pool) *pgStore`；`store.New(pool) Store` 保留为返回接口的兼容入口；`SetHistoryRetain` 归入接口（装饰器需透传）
- [x] 1.2 全仓引用点改构造：`go build ./...` 与 `go vet ./...` 通过（兼容入口允许暂不迁移的调用方）
- 验证：`go test ./internal/server/... -race` 全绿

## 2. handler 面向接口

- [x] 2.1 `handler.New` 参数与 `Server.store` 字段改为 `store.Store` 接口
- [x] 2.2 新增 `internal/server/handler/fakestore_test.go`：内存假 Store，覆盖 auth（有效/无效/被屏蔽 token）、metadata GET/PUT、pull/push（含冲突 409）、history 的请求-响应断言；不再依赖 testcontainers
- 验证：`go test ./internal/server/handler/ -race` 在无 docker 环境下全部可跑

## 3. 生命周期收拢

- [x] 3.1 `senv-server/main.go`：serve 进程单一 store 实例——`pruneAccessLogsPeriodically` 改收 `*store.pgStore`（或接口）复用同一实例，删除其内部 `store.New(pool)`；启动预检仍用独立 `pgx.Connect`（行为不变）
- [x] 3.2 admin 子命令 `withStore` 统一经 `NewSQL` 构造（保持一次性进程、无缓存）
- 验证：`go test ./... -race` 全绿；`make check` 通过

## 4. 回归与交接

- [x] 4.1 现有 store/handler 测试断言零修改（构造方式除外）——`git diff` 复核确认
- [x] 4.2 driver 验证记录补一行本切片完成状态
