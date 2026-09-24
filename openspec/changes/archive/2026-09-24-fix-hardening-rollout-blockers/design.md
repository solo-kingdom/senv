## Context

见 proposal.md - Why。两项缺陷同源于「加固项按单测语义写、未按真实部署语义写」：告警按「我能 POST 出去」定义成功，角色模板按「业务表授权齐全」定义可用，都没走通端到端。本次一并补齐端到端语义。

## Goals / Non-Goals

**Goals:** 告警能在飞书这类「200 但拒收」的网关上真实送达并被证明送达；受限角色下 serve 起得来且失败原因可读。

**Non-goals:** provider 适配层（不在 server 里内置厂商格式）；告警持久化补发。

## Decisions

1. **body 用模板而非 provider 枚举**：`text/template` + 一个 env，运维侧改格式无需发版。备选「内置飞书/Telegram 两种格式」被否——server 不该认识厂商，且飞书自身还要求群关键词（属团队约定，不该硬编码）。
2. **`missingkey=error` 而非默认**：让「模板写了不存在的字段」显式失败并丢该条，而不是悄悄渲染出 `<no value>` 送达——后者会把配置错误伪装成成功。为此 `alertTemplateData` 先预填全部已知字段再叠加原 JSON，`omitempty` 缺席的字段渲染成空串。
3. **envelope 判定白名单化到三个键**（`code`/`errcode`/`error_code`）：只认这三个通用约定，其余（含非 JSON body）一律按成功，避免把普通网关的 `{"status":"ok","code":0}` 之外的响应体误判成失败。
4. **SQLSTATE 判定替代子串匹配**：`errors.As(*pgconn.PgError)` + `42P01`。子串匹配在 42501 上必错判，且错误文案随 PG 版本/语言漂移。
5. **`schema_migrations` 只给 SELECT**：迁移执行权留在高权限角色，运行角色仅够自检版本；代价是每次 migrate 建新表须重跑 roles.sql 补授权，写进 roles.sql 注释与部署文档。

## Risks / Trade-offs

- [模板本身成为新的失败面] → 解析失败在构造期即回退默认 JSON 并记日志，不会静默丢掉全部告警
- [窥探响应体引入读body 成本] → `io.LimitReader` 上限 512B，且仅在告警投递（低频、独立 goroutine）路径上
- [重跑 roles.sql 遗漏 → 新表对运行角色不可见] → 负向验证 + 文档清单化（见部署侧 PLAN 的 B.2 步骤）
