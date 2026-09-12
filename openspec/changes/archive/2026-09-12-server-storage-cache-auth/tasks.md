## 1. 缓存 decorator

- [x] 1.1 `store/cache.go`：`WithCache(inner Store) Store`——auth 结果缓存（token SHA-256 哈希为键，TTL 30s）、vault 查找缓存（TTL 5min + cap 4096，读时惰性过期）、seq 缓存（push 更新）；`sync.Mutex + map`，不加 singleflight
- [x] 1.2 cache 假 Store 单测：命中/未命中/过期/全清/容量上限；`SetHistoryRetain` 等透传
- 验证：`go test ./internal/server/store/ -run TestCache -race` 全绿

## 2. NOTIFY 失效链路

- [x] 2.1 `pgStore.RevokeToken` / `SetClientStatus` 成功后执行 `SELECT pg_notify('senv_cache_invalidate','')`（同一连接；失败仅记日志不回滚业务写入）
- [x] 2.2 `store` 新增监听入口：专用连接 LISTEN + `WaitForNotification` 循环，断线固定间隔重连，**重连成功即全清**，context 取消即退出
- [x] 2.3 集成测试（testcontainers）：revoke/block/unblock 广播后已缓存结果的下一请求回库（revoke→401、block→403、unblock→恢复）
- 验证：`go test ./internal/server/store/ -race` 全绿

## 3. 装配与 spec 验证

- [x] 3.1 `main.go` serve 装配 `WithCache(NewSQL(pool))` 并启动监听 goroutine；admin 路径保持无缓存
- [x] 3.2 spec 场景逐条落测试：缓存命中零 auth 查询（driver D2）、吊销 ≤1 请求内 401、屏蔽穿透缓存 403、解封恢复、兜底 TTL 注入测试
- 验证：`go test ./internal/server/... -race` 全绿

## 4. benchmark 与交接

- [x] 4.1 `store/bench_test.go`：auth 命中/未命中 + Pull 全链路 benchmark（testcontainers PG，`-benchmem`）；before（unify 后无缓存）与 after 数字记入 driver 验证记录
- [x] 4.2 driver 验证记录补一行本切片完成状态；`make check` 通过
