## Purpose

定义 senv-server 的密文同步接口：vault metadata 托管、按 entry 乐观锁的批量推送、按 revision 增量拉取，server 对所有数据内容保持零知识。

## ADDED Requirements

### Requirement: vault metadata 托管

系统 SHALL 支持按 vault 读写 metadata blob（salt、passwordKey、settings 等，均为客户端产物的不透明数据）。写入 MUST 要求有效 token 且归属当前用户。

#### Scenario: 新机器拉取 metadata

- **WHEN** 客户端用有效 token 请求已存在 vault 的 metadata
- **THEN** 返回最近一次写入的 blob 原文

### Requirement: 乐观锁推送

推送接口 SHALL 接受批量条目，每条携带 `base_revision`。当某条目的 `base_revision` 与 server 当前 revision 不符时，MUST 整批拒绝并返回 409，响应中 MUST 列出冲突条目的标识与 server 端当前 revision，MUST NOT 部分写入。

#### Scenario: 无冲突推送

- **WHEN** 批量条目的 base_revision 均与 server 当前值一致
- **THEN** 全部写入，每条获得新的单调递增 revision，响应返回新 revision 集合

#### Scenario: 冲突整批拒绝

- **WHEN** 批量中任一 entry 的 base_revision 落后于 server 当前 revision
- **THEN** 返回 409 与冲突清单，整批无任何写入

### Requirement: 增量拉取

拉取接口 SHALL 接受 `since` 参数，返回该 vault 中 revision 大于 since 的全部条目（含删除标记），以及当前最新 revision。空增量 SHALL 返回空列表与最新 revision，不报错。

#### Scenario: 增量拉取含删除

- **WHEN** 某 entry 在上次同步后被删除
- **THEN** 拉取结果包含该 entry 的删除标记条目

#### Scenario: 无更新

- **WHEN** since 等于当前最新 revision
- **THEN** 返回空条目列表与相同 revision

### Requirement: revision 单调性

同一 vault 内 revision SHALL 单调递增且不重用；条目更新与删除都 MUST 推进 revision。

#### Scenario: 删除也推进 revision

- **WHEN** 删除某 entry
- **THEN** 该删除记录获得新 revision，之后以旧 since 拉取能观察到该删除

### Requirement: API 版本与 schema 校验

所有接口 SHALL 位于 `v1` 路径前缀下。server 启动时 MUST 校验数据库 schema 版本，不匹配时 MUST 拒绝启动并提示迁移方式。

#### Scenario: schema 版本不匹配

- **WHEN** 数据库 schema 版本低于 server 要求
- **THEN** 启动失败，错误信息指出当前版本、要求版本与迁移命令

### Requirement: 内容零知识

server MUST 将条目内容作为不透明密文字节存储与传输，MUST NOT 解析、索引或记录其明文含义。条目大小超限（单条超过 512KB 密文）时 MUST 拒绝并返回明确错误。

#### Scenario: 超限条目

- **WHEN** 推送包含超过大小上限的条目
- **THEN** 整批拒绝，错误指明超限条目标识与上限值
