## 1. 告警 body 模板

- [x] 1.1 `Options.AlertBodyTemplate` + env `SENV_SERVER_ALERT_BODY_TEMPLATE`（flag `--alert-body-template`），经 `newAlertDetector` 传入并在构造期 `text/template` 解析（`missingkey=error`）；解析失败记日志并留 nil = 回退默认 JSON
- [x] 1.2 `renderBody`：nil 模板走 `json.Marshal(payload)`（行为不变）；否则用 `alertTemplateData` 预填全部已知字段再叠加原始 JSON，使 `omitempty` 缺席字段渲染成空串而非 `<no value>`

## 2. 投递结果判定

- [x] 2.1 `webhookResultError(status, body)`：`status>=300` → 失败（附响应体片段）；2xx 时探测 `code`/`errcode`/`error_code`（数字或可解析字符串），非零 → 失败；非 JSON 或无该键 → 成功
- [x] 2.2 `deliver` 每次尝试都 `io.LimitReader` 窥探响应体（≤512B）并据 2.1 判定；保留 `lastErr`，重试耗尽的日志带原因

## 3. 受限角色 schema 版本校验

- [x] 3.1 `roles.sql`：`GRANT SELECT ON schema_migrations TO senv_server`，并注明「每次 migrate 建新表后须重跑本文件补授权」
- [x] 3.2 `migrate.CurrentVersion` 改用 `errors.As(*pgconn.PgError)` + SQLSTATE `42P01` 判定未初始化，替换原「错误信息含表名」子串匹配

## 4. 收尾

- [x] 4.1 单测 `alerts_body_test.go`：飞书形态渲染、模板为空行为不变、未知字段零投递、语法错误回退、envelope 判定表驱动、200+19024 恰好 3 次尝试
- [x] 4.2 集成测 `roles_schemaversion_test.go`（testcontainers PG）：受限角色过版本校验；REVOKE 后报权限错误而非静默返回 0
- [x] 4.3 `docs/senv-server.md` 告警节补模板示例与「HTTP 200 ≠ 投递成功」；`.agents/skills/senv-cli/SKILL.md` 同步新 flag
