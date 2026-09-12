## 1. TouchClient 内存节流

- [x] 1.1 decorator 内实现 touch buffer：`TouchClient` 记内存 map 即返回；flush goroutine 每 1 分钟批量发原 UPDATE（SQL 谓词保留）；`FlushTouches(ctx, timeout)` 供停机调用
- [x] 1.2 单测：1 分钟内 N 次 Touch 仅 flush 1 次写；flush 前崩溃语义（buffer 丢弃）不 panic；admin 无缓存路径不受影响
- [x] 1.3 serve 停机链路挂 `FlushTouches`（优雅停机后、进程退出前，5s 超时 best-effort）
- 验证：`go test ./internal/server/store/ -run TestTouch -race` 全绿

## 2. ADR 与文档

- [x] 2.1 落盘 `docs/adr/NNNN-server-cache-out-of-band-invalidation.md`（编号扫描 `docs/adr/` 取下一空位；内容按 design 决策 3）
- [x] 2.2 `docs/senv-server.md` 补「单实例内存态」部署约束段（限速窗口 + 认证缓存 + 多副本需重新设计失效）
- [x] 2.3 driver `grill.md` ADR 候选勾选并注明落盘编号；driver 验证记录补本切片完成状态
- 验证：`make check` 通过
