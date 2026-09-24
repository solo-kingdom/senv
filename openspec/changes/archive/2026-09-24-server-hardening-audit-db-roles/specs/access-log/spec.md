## ADDED Requirements

### Requirement: 管理员操作审计

admin CLI 子命令（创建用户、吊销 token、签发注册码、屏蔽/解封 client）成功执行后 SHALL 写一条安全事件：`outcome=ADMIN`，`reason` 编码操作类型与目标对象，`user_id`/`client_id` 填被操作对象。写入 MUST 为 best-effort：失败仅记服务端日志，不影响命令结果与退出码。事件 MUST NOT 含 token、口令或密文内容。

#### Scenario: 创建用户被审计
- **WHEN** 管理员执行 create-user alice 成功
- **THEN** access_log 新增一条 ADMIN 事件，reason 含操作类型与目标用户名

#### Scenario: 吊销 token 被审计
- **WHEN** 管理员执行 revoke-token 成功
- **THEN** 新增一条 ADMIN 事件，记录目标用户与吊销操作

#### Scenario: 审计写入失败不影响命令
- **WHEN** ADMIN 事件写入失败
- **THEN** 命令仍按其原语义成功退出，仅服务端日志记录错误

### Requirement: 访问日志角色隔离契约

系统 SHALL 提供受限数据库角色模板：serve 运行时角色对 access_log 仅持有 INSERT 与 SELECT，不持有 UPDATE/DELETE（`admin logs-prune` 需使用单独的高权限角色）。模板 MUST 为单一事实源，供部署文档引用。

#### Scenario: 受限角色不可篡改日志
- **WHEN** 以受限角色连接并对 access_log 执行 UPDATE/DELETE
- **THEN** 数据库拒绝该操作

#### Scenario: 受限角色可正常记录
- **WHEN** 以受限角色执行 INSERT/SELECT
- **THEN** 操作成功，serve 正常读写访问日志
