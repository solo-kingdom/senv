## Context

见 proposal.md - Why 与 driver design.md 决策 1。约束：角色隔离是部署期配置，代码层唯一可做的是把 ADMIN 审计与 GRANT 模板作为「建议契约」提供并测试。

## Goals / Non-Goals

**Goals:** ADMIN outcome 值与五个子命令的审计写入可测试；GRANT 模板成为单一事实源（迁移目录 SQL 或文档代码块，其余切片引用同一处）。

**Non-Goals:** 运行时角色强制；cron 示例（deploy-docs 切片）。

## Decisions

1. **ADMIN 事件复用 access_log 而非新表**：查询/清理/截断工具链零改动，`admin logs --outcome ADMIN` 天然可用。备选「独立 admin_log 表」被否：多一套清理与过滤逻辑，收益为零。
2. **reason 编码操作**：`reason` 上限 128 字节，操作类型 + 目标名足够（用户名/设备名 ≤128）。解析展示侧按空格切首段即可，不引入新列。
3. **user_id/client_id 填被操作对象而非操作者**：admin CLI 无操作者身份概念（持有 DSN 即管理员），填目标对象让 `admin logs --user` 过滤直接命中。

## Risks / Trade-offs

- [现有部署未用受限角色，模板形同虚设] → deploy-docs 切片把「生产必须用受限角色」写进部署检查清单
- [ADMIN 事件被同一 retention 清理] → 可接受，日志本就 best-effort
