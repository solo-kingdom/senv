---
name: server-hardening
title: senv-server 公网加固探索
slug: server-hardening
status: archived
created: 2026-09-24
updated: 2026-09-24
archived: 2026-09-24
handed-off: 2026-09-24
driver: server-hardening-driver
---

# senv-server 公网加固探索

## 目标

收敛 senv-server 公网部署的安全加固功能集并冻结决策，交接 taskflow 实现：审计日志可信性（DB 角色隔离 + admin 操作入日志）、通用 webhook 告警、token HMAC pepper、部署加固文档。

## 非目标

- 不改零知识架构本身（密文托管模型不动）
- 本阶段不写实现代码
- 缓议不做：per-token 限速、存储配额、token TTL/轮换、hash chain、mTLS

## 现状

已有防线（查证于 `internal/server/handler/`、`internal/server/store/accesslog.go`、`senv-server/main.go`、`docs/senv-server.md`）：

- 零知识密文托管，server 只存密文 + token SHA-256 哈希
- Bearer 认证 + 每 IP 认证失败限速（`/v1/register` 共用同一限速器）
- 每请求访问日志落 Postgres（90 天保留，字段截断防灌爆）
- `MaxBytesReader` 64MB、`http.Server` 四超时齐全、`trust-proxy-headers` fail-closed
- 反代终结 TLS，客户端默认强制 https

已知短板：

- 访问日志与业务数据同库同角色，拿到 DSN 可抹痕迹
- admin CLI（create-user/revoke/block）不留任何日志
- 无异常告警，日志被动等人查
- token 哈希无 pepper，DB 整库泄露可离线反查

约束（用户确认）：

- 威胁模型：单人/小团队自用，公网随机扫描 + 偶发定向
- 单实例部署；多实例不支持（内存态限速器），文档注明边界
- 接受双 DSN + cron 运维 `logs-prune`

## 方案

纳入范围四项：

1. **DB 角色权限隔离 + admin 操作入日志**：serve 运行时角色对 `access_log` 仅 `INSERT + SELECT`；`logs-prune` 用单独 admin 角色（cron 跑）；admin 子命令写审计记录
2. **通用 webhook 告警**：server 提供可配置 webhook URL，异常事件 POST JSON（连续 AUTH-FAILED 超阈值、BLOCKED、新 client 注册、client 换 IP 首次访问）；不内置第三方 provider
3. **token HMAC pepper**：SHA-256 改 HMAC，pepper 来自环境变量/文件，不进 DB
4. **部署加固文档**：systemd 加固、反代推荐配置（HSTS、limit_req、TLS 1.2+）、双 DSN cron 示例、多实例边界、元数据泄露边界（vault 名/同步频率可见）写入 `docs/senv-server.md`

- 步骤：四项可独立实现/验证；文档项随其余项同步落
- 阻塞点：无
- 坑：pepper 变更会使旧 token 哈希失效（需与轮换策略一起考虑）；DB 角色隔离要求迁移脚本不依赖运行时角色建表

## 进展

- 2026-09-24：初步发散完成，六个方向清单
- 2026-09-24：grilling 收敛，冻结四项范围 + 约束（见决策）

## 决策

- 采纳：四项最小高收益组合（DB 角色隔离+admin 审计、webhook 告警、HMAC pepper、部署文档）— 半天到一天粒度、收益明确
- 取舍：接受双 DSN + cron 运维代价；放弃重防护项（per-token 限速、配额、token TTL、hash chain、mTLS）入本轮
- 带进实现的未决：元数据泄露边界写进文档哪一节（实现期定）
- 回退：pepper 引入导致旧 token 失效时，提供重新签发流程即可整体回退；角色隔离回退即恢复单角色

## 交接

- driver: `server-hardening-driver`
- 采纳方案：四项最小高收益组合（DB 角色隔离 + admin 操作入审计、通用 webhook 告警、token HMAC pepper、部署加固文档），详见本文件「方案」「决策」小节
- design/ 指针：无设计稿，方案全文在 TASK.md「方案」小节
- 可带进实现的未决：元数据泄露边界在 `docs/senv-server.md` 的落点小节
- 归档路径：`tasks/archive/2026-09-24/server-hardening/`

## 未决问题

- [ ] 元数据泄露边界在 `docs/senv-server.md` 的落点小节（实现期定）

## 下一步

- decide（本轮完成）→ handoff
