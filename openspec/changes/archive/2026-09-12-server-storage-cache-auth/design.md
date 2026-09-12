## Context

driver grill 决策 D2/D8/D10 为本切片的设计输入；接口缝由 `server-storage-cache-unify` 提供（`Store` 接口 + `NewSQL(pool) *pgStore`）。现状热路径：每请求 `AuthenticateWithClient`（1 查询）+ `TouchClient` 往返（touch 切片处理）+ access log INSERT（不动）；Pull 另有 lookupVault + seq + 范围查询 3 次。

## Goals / Non-Goals

- Goals：auth/vault/seq 三类缓存 + NOTIFY 失效链路 + benchmark 证据
- Non-Goals：singleflight、多副本失效、entries/metadata/access log 缓存、access log 异步化

## Decisions

1. **decorator 形态**（D9）：`store/cache.go` 的 `WithCache(inner Store) Store` 包住任意 `Store` 实现；缓存字段 `sync.Mutex + map`，不加 singleflight（D11）。拒绝把缓存埋进 pgStore：无法用假实现单测缓存逻辑。
2. **失效 = 广播全清**（D8）：NOTIFY 载荷不带具体 token，收到即清整个 auth map。理由：revoke/block 极 rare（管理操作），全清最简单且不会漏（block 影响该 client 全部 token，逐条失效反而要反查映射）；`pg_notify` 在 admin 进程的同一事务提交后送达。
3. **admin 发通知的落点**：`RevokeToken` 与 `SetClientStatus`（block 与 unblock 都要——解封若不广播，已缓存的 403 状态会滞留到 TTL）在 SQL 执行成功后 `SELECT pg_notify('senv_cache_invalidate', '')`；通知失败仅记日志（兜底 TTL 保证最终一致）。
4. **serve 端监听**：`store.NewListener(dsn)` 起独立 goroutine 持专用连接（pgx `WaitForNotification`），断线按间隔重连并在**每次重连成功时全清缓存**（压缩丢失窗口）；serve 停机时随 context 取消退出。
5. **seq 缓存方向性安全**（D10）：缓存偏旧 = 值更小 = 「客户端已最新」快捷判断不成立 = 落回 DB 范围查询，无假「已最新」；push 路径（同进程）更新缓存。vault 查找缓存无失效（全仓无 vault 删除路径，已验证）。
6. **TTL 与容量**（D10）：auth 条目 TTL 30s（= 兜底上限，主动过期仅是兜底不是主通道）；vault 条目 TTL 5min + 容量 4096（防病态膨胀，非性能旋钮）。惰性过期（读时检查）即可，不后台清扫。
7. **benchmark**（D7）：`store/bench_test.go` 用 testcontainers PG 对 `AuthenticateWithClient`（缓存命中/未命中）与 Pull 全链路跑 `b.ReportAllocs`；before/after 数字贴 driver 验证记录。基准跑法固定：`go test -bench . -benchmem ./internal/server/store/`。
8. **不缓存 vault 名→vaultID 映射（实现期修正 grill D2）**：设计阶段计划的「vault 查找缓存」在 decorator 接缝上不可实现——`Store` 接口各方法均不返回 vaultID，decorator 学不到 name→id 映射；为其加宽接口等于为低频路径（metadata/history）付接口税。原动机已被 seq 快捷判定覆盖：pull「已最新」时一次性省掉 lookupVault + seq 读 + 范围查询全部 3 次往返。拒绝的备选：把该缓存埋进 pgStore 内部（违背决策 1 的可测性理由：decorator 用假实现单测，pgStore 内嵌缓存测不到缓存逻辑）。

## Risks / Trade-offs

- [NOTIFY 丢失 → 已屏蔽 client 至多 30s 内仍可通过认证] → 兜底 TTL 上限写进 spec（server-auth ADDED）与 ADR；重连全清压缩窗口；若业务上不可接受，退路是去掉 auth 缓存（D8 备选 c）
- [监听 goroutine 泄漏/重连风暴] → 重连间隔固定退避；serve 停机经 context 优雅退出
- [缓存 key 用 token 哈希，内存驻留增加攻击面?] → 哈希本就是 server 唯一持有形态，进程内存不落盘，与 `tokens` 表同级别驻留
- [decorator 包住 `SetHistoryRetain` 等配置 setter 需透传] → 接口已含该 setter，decorator 转发 inner

## Migration Plan

serve 装配改为 `store.NewSQL(pool)` → `store.WithCache(base)` → handler；admin 路径保持 `NewSQL` 无缓存。回滚 = revert 并回到 `NewSQL` 直连装配；无 schema 迁移。

## Open Questions

无
