## Why

`TouchClient` 目前每请求一次 UPDATE 往返（`clients.go:214-221`，SQL 内 1 分钟节流只省写不省往返）。`last_seen_at` 是纯展示字段（admin `list-clients` 查看），无任何安全语义——这是每请求固定开销里唯一白捡的优化。同时把 driver 的 ADR 候选与部署文档落盘收尾。

## What Changes

- `TouchClient` 内存化节流：last_seen 先记进程内 map，至多 1 分钟批量 flush 一次；server 优雅停机时 best-effort flush 一次
- 展示语义：admin `list-clients` 的 LAST_SEEN 延迟至多约 1 分钟（原 SQL 节流本来就是 1 分钟粒度，观感不变）；server 崩溃时丢失未 flush 的更新（字段无安全语义，可接受）
- ADR 落盘：`docs/adr/NNNN-server-cache-out-of-band-invalidation.md`（编号按 `docs/adr/` 扫描取下一空位；与 `cross-machine-ai-mcp-sync-driver` 的候选按落地先后取号）
- `docs/senv-server.md` 补单实例姿态说明：auth 缓存与 rate limiter 同为"单实例内存态"，多副本部署需一并重新设计失效
- driver 验证记录补 ADR 落盘记录

## Non-goals

- 不做停机前的定时持久化（last_seen 无持久化价值）
- 不改 access log 写路径、不缓存任何新数据
- 不改 `TouchClient` 的 SQL 节流谓词（保留为 flush 后的写侧防线）

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

（无——`last_seen` 在既有 spec 中无任何要求，节流不改变 API 行为；`.openspec.yaml` 已设 `skip_specs: true`）

## Impact

- `internal/server/store/`（TouchClient 节流与 flush）、`senv-server/main.go`（停机 flush 挂钩）、`docs/adr/`、`docs/senv-server.md`
