# 单实例 server 的认证缓存失效采用 LISTEN/NOTIFY 广播

senv-server 引入进程内认证缓存（driver `server-storage-cache`）后，「屏蔽/吊销即时生效」面临一个部署拓扑事实：管理操作（`admin revoke-token` / `block-client` / `unblock-client`）运行在 `senv-server admin` 一次性进程中，与 serve 进程完全隔离（无 HTTP admin 端点），serve 进程的内存缓存无法被同进程写路径同步失效。我们采用 PostgreSQL LISTEN/NOTIFY：admin 路径写库成功后 `pg_notify` 广播，serve 进程持专用连接监听、收到即清空整个认证缓存（管理操作极 rare，全清最简单且不漏）；缓存 TTL（30s）仅作广播丢失的兜底，监听重连成功即再全清一次。拒绝的备选：纯 TTL 缓存——必须把「屏蔽 SHALL 使该 client 名下所有凭证立即失效」（server-clients spec）弱化为有界窗口才能自洽，安全承诺不为实现让路；放弃缓存 auth——白丢每请求最大头的固定开销（基准 ≈317×）。单实例内存态姿态与 rate limiter 先例一致；扩多副本时认证缓存与限速器需一并重新设计失效。

## Consequences

- 最坏窗口：广播丢失（监听瞬断）且 TTL 未到期时，已屏蔽 client 至多 30s 内仍可通过认证。这是本方案相对同进程失效的已知代价，由 server-auth spec「认证结果缓存」显式规定并测试覆盖；若业务不可接受，退路是去掉 auth 缓存。
- serve 进程多占一条数据库连接（监听专用），断线按固定间隔重连。
