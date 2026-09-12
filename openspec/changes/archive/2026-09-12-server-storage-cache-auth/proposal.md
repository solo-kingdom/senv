## Why

server 每个受保护请求固定跑一次 `AuthenticateWithClient` 查询（tokens LEFT JOIN clients），加上 vault 查找与 seq 读取，是 Pull/Metadata/History 路径的主要 DB 开销。tokens/clients 读极重、写极稀（仅签发/吊销/屏蔽），适合进程内缓存。本切片在 `server-storage-cache-unify` 留出的接口缝上落地缓存与失效链路。

## What Changes

- 新增 `store/cache.go`：`WithCache(inner Store) Store` decorator，缓存两类数据——auth 结果（token SHA-256 哈希为键 → AuthResult）与 vault seq（按 userID+vault，支撑「已最新」快捷判定）
- seq 快捷判定：since ≥ 缓存 seq 时直接返回空增量，一并省掉 lookupVault + seq 读 + 范围查询共 3 次往返；seq 缓存偏旧只会退化为完整范围查询（不产生假「已最新」）；push 同进程即时更新 seq
- auth 失效：serve 进程持专用连接 LISTEN 失效通道；admin 路径 `RevokeToken` / `SetClientStatus`（含屏蔽与解封）执行后 `pg_notify` 广播；serve 收到即清空全部 auth 缓存；监听重连时也全清；30s TTL 仅作 NOTIFY 丢失兜底
- serve 进程接线：`handler` 使用带缓存 store；admin 一次性进程保持无缓存
- benchmark：auth+pull 热路径前后对比数字记入 driver 验证记录

## Non-goals

- 不缓存 vault 名→vaultID 映射：`Store` 接口各方法均不返回 vaultID，decorator 接缝学不到该映射；其唯一收益点（metadata/history 低频路径）不值得为此加宽接口（实现期发现，见 design 决策 8；pull 快捷判定已覆盖其主要动机）
- 不缓存 entries 密文、vault metadata、access log
- 不加 singleflight/击穿防护（benchmark 显示锁竞争再补）
- 不做多副本失效（单实例姿态，见 driver D6）
- 不动 access log 同步写与 TouchClient（后者属 `server-storage-cache-touch`）

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `server-auth`: 新增「认证结果缓存」需求——缓存不得弱化吊销/屏蔽语义、失效机制与兜底上限、缓存内容边界
- `server-clients`: 修订「屏蔽与解封」需求——「立即失效」在缓存语境下细化为失效广播即时生效 + 有界兜底窗口；解封同样即时生效

## Impact

- `internal/server/store/`（新增 cache.go 与 NOTIFY 监听）、`internal/server/store/clients.go`（admin 写路径发通知）、`senv-server/main.go`（LISTEN 接线、serve/admin 缓存装配差异）
- Postgres：新增一个 LISTEN 通道（无 schema 变更、无迁移）
