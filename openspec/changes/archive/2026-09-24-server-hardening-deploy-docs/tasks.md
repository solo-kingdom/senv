## 1. 文档

- [x] 1.1 `docs/senv-server.md` 新增「公网加固」节：systemd 单元示例、反代推荐配置（HSTS/limit_req/TLS 1.2+）、双 DSN cron 示例、单实例边界、元数据泄露边界
- [x] 1.2 pepper 轮换指引：启用后 N 天内全量轮换（revoke + 重新签发）、pepper 备份要求、慢日志核对方法
- [x] 1.3 GRANT 模板引用 audit-db-roles 单一事实源；交叉核对三个切片实际 flag/env 名
