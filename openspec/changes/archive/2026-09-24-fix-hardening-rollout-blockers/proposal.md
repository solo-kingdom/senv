## Why

四项加固（#19）落地后首次在真实部署（tcbj 公网实例）上演练滚更，发现两个会让部署失败或静默失效的缺陷：

1. **告警链路对通用 webhook 的「成功」判定过宽**。`deliver` 只看 HTTP 状态码，而飞书自定义机器人这类网关在拒绝消息（缺少群关键词、限流）时返回 **HTTP 200 + `{"code":19024,"msg":"Key Words Not Found"}`**——server 认为投递成功，管理员以为告警在，实际一条都收不到。同时它的 body 结构是 provider 专有的（`{"msg_type":"text","content":{"text":…}}`），检测器自身那份扁平 JSON 无法直接投。既有 fleet 里的生产者（如 `deploy/tcse/github-secret-scan/scan.py`）都是自己拼 body、并回查 body 里的 `code`，缺的这一环只能在 server 侧补。
2. **受限运行角色起不来服务**。`serve` 启动即 `migrate.CheckCurrent` 校验 schema 版本，而 `roles.sql` 未授 `schema_migrations` 的 SELECT；更糟的是 `CurrentVersion` 用「错误信息含表名」的子串匹配把「未初始化」和「权限不足」混为一谈——`permission denied for table schema_migrations` 同样含表名，于是受限角色下被判成未初始化，serve 以误导性的「schema 版本不匹配」退出。

实现对照：`internal/server/handler/alerts.go`（`renderBody`/`deliver`/`webhookResultError`）、`internal/server/migrate/migrate.go`（`CurrentVersion`）、`senv-server/sql/roles.sql`。

## What Changes

- serve 新增 `SENV_SERVER_ALERT_BODY_TEMPLATE`（flag `--alert-body-template`）：以 `text/template` 渲染告警请求体，未配置时保持原扁平 JSON 行为逐字节不变
- 模板变量为 payload 字段（`alert`/`time`/`ip`/`user_id`/`client_id`/`user`/`client`/`reason`/`count`）；解析失败回退默认 JSON + 日志，渲染失败（含未知字段）丢弃该条 + 日志
- 投递结果改为按响应内容判定：2xx 且响应体 JSON 的 `code`/`errcode`/`error_code` 非零 → 判失败走重试；非 JSON 或无该字段 → 判成功；最终丢弃的日志带状态码与响应体片段
- `roles.sql` 给 `senv_server` 补 `GRANT SELECT ON schema_migrations`（迁移仍由高权限角色执行，故只给 SELECT）
- `CurrentVersion` 改按 SQLSTATE `42P01` 判定未初始化，权限不足（42501）如实返回错误

## Non-goals

- 内置任何具体 provider（飞书/Telegram 的 body 结构由运维用模板表达，server 不识别厂商）
- 告警投递的持久化/离线补发（仍是 best-effort：进程退出即丢在途告警）
- 多实例部署支持

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | senv-server：`handler/alerts.go` + `migrate/migrate.go` + `senv-server/main.go` + `sql/roles.sql` |

## 验收标准

- [x] 配置飞书形态模板后 POST body 为该模板渲染结果且含事件元数据；模板为空时 body 与原 JSON 一致
- [x] 模板语法错误 → 回退默认 JSON 投递；模板引用未知字段 → 该条丢弃并记日志，不 panic
- [x] 200 + `{"code":19024}` 判定为失败并重试（测试断言恰好 3 次尝试）；200 + `ok`/`{}` 判定为成功
- [x] 受限角色（仅 `USAGE ON SCHEMA` + `SELECT ON schema_migrations`）下 `CurrentVersion` 返回最新版本、serve 可过版本校验；REVOKE 后返回权限错误而非 0
- [x] `go test ./internal/server/... ./senv-server/...` 全绿
