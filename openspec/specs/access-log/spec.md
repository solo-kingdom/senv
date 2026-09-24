# access-log Specification

## Purpose
server 将每个 API 请求的认证与处理结果落库为安全事件流水，供服务器管理员追溯访问尝试、登录成功/失败、屏蔽拦截、限速与同步行为。
## Requirements
### Requirement: 每请求安全事件落库
server SHALL 将每个受保护 API 请求（健康检查除外）记录为一条安全事件：时间、来源 IP、方法与路径、可解析时的 client 与 user 标识、结果（OK / AUTH-FAILED / BLOCKED / RATE-LIMITED）；AUTH-FAILED SHALL 附失败原因。记录 MUST NOT 包含 token、口令或任何密文内容。

#### Scenario: 正确凭证访问
- **WHEN** 持有效凭证的请求处理成功
- **THEN** 记录一条结果为 OK 的事件，含时间、来源 IP 与身份标识

#### Scenario: 错误凭证访问
- **WHEN** 请求携带无效 token
- **THEN** 记录一条 AUTH-FAILED 事件并附原因，401 响应语义不受影响

#### Scenario: 被屏蔽 client 访问
- **WHEN** 已屏蔽 client 的请求被拒
- **THEN** 记录一条 BLOCKED 事件

#### Scenario: 限速拦截
- **WHEN** 同一来源失败超限被返回 429
- **THEN** 记录一条 RATE-LIMITED 事件

### Requirement: 日志记录不阻断服务
安全事件写入 SHALL 为 best-effort：写入失败时请求照常处理，仅记服务端错误日志。

#### Scenario: 日志库写入失败
- **WHEN** 安全事件写入失败
- **THEN** 该请求的 API 响应不受影响

### Requirement: 管理员查询与清理
系统 SHALL 提供仅限服务器管理员的 CLI 查询（按用户、client、起止日期、结果与条数过滤，输出含日期时间）与按日期清理命令；安全事件 MUST NOT 可经 client API 查询。

#### Scenario: 按日期与结果查询
- **WHEN** 管理员查询某日期之后的 AUTH-FAILED 事件
- **THEN** 输出匹配事件列表，每条含日期时间、来源 IP、身份与原因

#### Scenario: 清理旧事件
- **WHEN** 管理员执行按日期清理
- **THEN** 仅删除该日期之前的事件，其后事件保留

#### Scenario: client 不可查询
- **WHEN** client 尝试查询安全事件
- **THEN** 不存在对应 API 端点

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

