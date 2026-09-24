# access-log Specification

## MODIFIED Requirements

### Requirement: 访问日志角色隔离契约

系统 SHALL 提供受限数据库角色模板：serve 运行时角色对 access_log 仅持有 INSERT 与 SELECT，不持有 UPDATE/DELETE（`admin logs-prune` 需使用单独的高权限角色）。模板 MUST 为单一事实源，供部署文档引用。受限角色 MUST 能支撑 serve 启动：模板 SHALL 授予 `schema_migrations` 的 SELECT（版本校验所需；迁移本身仍由高权限角色执行，故不给 DDL/写入），且版本探测代码 MUST 按数据库错误码区分「schema 未初始化」与「权限不足」，不得把后者误判为前者。

#### Scenario: 受限角色不可篡改日志

- **WHEN** 以受限角色连接并对 access_log 执行 UPDATE/DELETE
- **THEN** 数据库拒绝该操作

#### Scenario: 受限角色可正常记录

- **WHEN** 以受限角色执行 INSERT/SELECT
- **THEN** 操作成功，serve 正常读写访问日志

#### Scenario: 受限角色可通过启动时版本校验

- **WHEN** 以模板授予的受限角色启动 serve（schema 已是最新版本）
- **THEN** 版本校验通过、服务正常启动；若该角色被撤销 `schema_migrations` 的 SELECT，则启动以权限错误失败而非误报「schema 未初始化/版本不匹配」
