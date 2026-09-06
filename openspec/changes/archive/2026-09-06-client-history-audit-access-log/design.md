## Context
server 路由注册在 internal/server/handler/handler.go:70-75（Go 1.22 ServeMux），认证中间件 handler.go:85-112，限速器 handler.go:27-29 + internal/server/handler/ratelimit.go（内存固定窗口，失败计数）；现有 slog 仅打服务端错误（handler.go:105 等）。admin 子命令在 senv-server/main.go:155-198。依赖：client-history-audit-identity 的 clients 表（本 change 排在其后实施）。决策依据：driver grill.md（D6、D7）。

## Goals / Non-Goals
**Goals:** 每请求安全事件落库（含认证失败原因与屏蔽/限速拦截）、admin 查询与按日期清理
**Non-Goals:** client 查询 API、自动保留策略、stdout 日志改造、采样（见 proposal）

## Decisions
1. **中间件位置与记录范围**：在最外层包一个 access-log 中间件（含限速后的 429），healthz 跳过；结果枚举 OK/AUTH-FAILED/BLOCKED/RATE-LIMITED 由内层通过 response wrapper 标注（认证失败原因在认证中间件内写入 request context，外层统一取用）。
2. **表结构与索引**：access_log(id, ts, ip, method, path, client_id NULL, user_id NULL, outcome, reason)，索引 (ts)、(user_id, ts)、(client_id, ts)、(outcome)；client_id/user_id 依认证解析结果，未解析为 NULL（含 401 场景）。
3. **写入方式 best-effort + 内联**：请求处理完成后同步单行 INSERT，错误仅 slog 记服务端错误。备选「异步队列」在低 QPS 个人部署下收益小、复杂度高，弃。
4. **查询/清理仅 admin CLI**：`senv-server admin logs`（--user --client --since --until --outcome --limit，时间含日期输出）与 `senv-server admin logs-prune --before <日期>`；不注册任何 HTTP 端点。
5. **同步事件**：push/pull 请求天然落库（method+path 可区分），不单独埋点；查询端 --outcome OK 且 path 匹配即可追溯同步行为。

## 数据流
```
请求 ──▶ access-log 中间件 ──▶ 限速器 ──▶ 认证中间件(失败原因写 context) ──▶ handler
  ▲                                                        │
  └──(响应后) 取 outcome/身份/原因 ──▶ INSERT access_log（失败仅 slog）
admin CLI: logs 查询 / logs-prune --before ──▶ store 直查/删除
```

## 错误处理策略
- INSERT 失败：slog 记错误，API 响应不变（spec「不阻断服务」）
- --before/--since 日期解析失败：命令报错退出非零，不做模糊处理
- prune 使用分批删除（LIMIT 循环）避免长事务锁表

## CLI 使用示例
```
senv-server admin logs --since 2026-09-01 --outcome AUTH-FAILED --limit 100
# → 表格输出：时间(含日期) | IP | method path | client/user | 结果 | 原因
senv-server admin logs --user alice --client my-laptop
senv-server admin logs-prune --before 2026-07-01
```

## Risks / Trade-offs
- [表无限增长] → 手动 logs-prune 收口（grill D6：v1 不做自动保留）；索引保证查询/删除性能
- [每请求一次 INSERT 的开销] → 个人部署 QPS 极低，可接受；如成瓶颈再引入批量写
- [IP 经反代失真] → 取标准 X-Forwarded-For 首值仅当配置显式信任代理时（v1 记 RemoteAddr，代理场景记为已知限制）

## Migration Plan
0004 仅加表；上线即开始记录，无回填。回滚：回退二进制后停止记录，残留数据可留待后续 prune。

## Open Questions
无
