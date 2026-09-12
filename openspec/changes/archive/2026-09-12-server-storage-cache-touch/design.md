## Context

`TouchClient`（`internal/server/store/clients.go:214-221`）现状：每请求一次 UPDATE 往返，SQL 谓词 `last_seen_at < now() - interval '1 minute'` 把真实写库压到 ≤1 次/分钟/client，但往返本身每次都发生。driver grill D5：内存化节流，flush 周期 1 分钟与 SQL 节流粒度对齐。

## Goals / Non-Goals

- Goals：省掉每请求 1 次 UPDATE 往返；ADR 与部署文档收尾
- Non-Goals：last_seen 持久化、访问日志异步化、任何新缓存

## Decisions

1. **节流落点在 decorator**（`WithCache` 内新增 touch buffer）而非 pgStore：与 auth 切片的缓存同层，admin 一次性进程（无缓存）保持每请求直写——admin 命令本来就不该更新 last_seen，实际不触碰此路径。装饰器 `TouchClient` 改为：记录 `(clientID, now)` 进 map 即返回；flush goroutine 每 1 分钟把 map 内每 client 发一次原 UPDATE（SQL 谓词保留，双保险）。
2. **停机 best-effort flush**：serve 优雅停机链路（`main.go` Shutdown 后）调用一次 `FlushTouches(ctx, 5s)`；失败仅记日志。
3. **ADR 内容**（D8/D12）：标题「auth 缓存的失效广播」——单实例 server 为何用 pg LISTEN/NOTIFY（admin 是独立一次性进程，同进程失效不成立）；三门槛与备选（TTL-only 弱化「屏蔽即刻失效」承诺 / 放弃 auth 缓存）各一段；Consequences 记 30s 最坏兜底窗口与重连全清。
4. **ADR 编号**：落盘时扫描 `docs/adr/` 取下一空位；`cross-machine-ai-mcp-sync-driver` 的候选 `sync-ai-mcp-source-of-truth` 若先落地则本篇顺延，无冲突。
5. **docs/senv-server.md**：「构建与发布」附近补一段部署约束——单实例内存态（限速窗口 + 认证缓存），多副本前需重新设计失效（LISTEN/NOTIFY 广播语义不跨实例）。

## Risks / Trade-offs

- [server 崩溃丢未 flush 的 last_seen 更新] → 字段纯展示、无安全语义；丢失窗口 ≤1 分钟
- [flush 与活跃请求并发写同一 client 行] → 原 UPDATE 幂等且 SQL 谓词节流，无锁需求
- [ADR 与另一 driver 候选抢编号] → 按落地先后取号，两处 grill.md 均已注明

## Migration Plan

无数据/协议变化；回滚 = revert，回到每请求直写路径。

## Open Questions

无
