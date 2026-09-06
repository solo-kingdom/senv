# server-history Specification

## Purpose
server 模式下按条目保留最近 N 个密文历史版本，支持按条目回看（含日期与内容对比）与单条目恢复，避免误改误删不可找回。
## Requirements
### Requirement: 条目历史留存
server 在条目被推送修改或删除前 SHALL 将其当前密文版本写入该条目历史；每个条目 SHALL 仅保留最近 N 个历史版本（N 默认 3，可由 server 启动参数配置），超出时按 revision 由新到旧裁剪。

#### Scenario: 修改产生历史
- **WHEN** 某条目被成功推送修改
- **THEN** 修改前的密文与 revision 进入该条目历史，且该条目历史版本数不超过 N

#### Scenario: 删除产生可找回历史
- **WHEN** 某条目被推送删除（tombstone）
- **THEN** 删除前的最后密文保留在该条目历史中

### Requirement: 历史查询端点
认证用户 SHALL 能查询自己 vault 内任意条目的历史版本列表（标识、revision、服务端时间戳、密文；已删除条目返回其最后版本），以及 vault 级按时间倒序的最近历史变更；只能查询自己 vault，跨用户访问 MUST 返回 404。

#### Scenario: 查看某条目历史
- **WHEN** client 请求某条目的历史
- **THEN** 返回按 revision 新到旧的版本列表，含时间戳与密文

#### Scenario: 跨用户查询被拒
- **WHEN** 用户请求他人 vault 的历史
- **THEN** 返回 404，与 vault 不存在时一致

### Requirement: 历史查看（cli+tui）
client SHALL 提供 `senv history` 命令与 TUI 历史视图：不带参数列出 vault 最近历史变更（含日期时间），指定条目时按时间新到旧列出各版本（本地解密展示，含日期时间）并可与当前值对比；无历史时明确提示而非报错。

#### Scenario: cli 查看条目历史
- **WHEN** 用户执行 `senv history` 并指定某条目
- **THEN** 按时间新到旧列出各版本的日期时间与解密内容，并标示与当前值的差异

#### Scenario: tui 查看历史
- **WHEN** 用户在 TUI 打开条目历史视图
- **THEN** 展示版本时间线（含日期时间）与选中版本的解密内容

### Requirement: 单条目恢复
client SHALL 支持将某历史版本恢复为当前值：按本地解密→写入本地→既有推送流程进行；已删除条目的恢复等价于以历史密文重新创建。恢复 MUST 复用乐观锁与冲突检测，MUST NOT 绕过。

#### Scenario: 恢复被误改的条目
- **WHEN** 用户选择某历史版本执行恢复
- **THEN** 条目当前值变为历史值，经既有推送流程同步并产生新的 revision

#### Scenario: 找回已删除条目
- **WHEN** 用户恢复已删除条目的最后历史版本
- **THEN** 该条目以历史内容重新出现并正常同步

