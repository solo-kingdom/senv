## MODIFIED Requirements

### Requirement: 乐观锁推送

推送接口 SHALL 接受批量条目，每条携带 `base_revision`。当某条目的 `base_revision` 与 server 当前 revision 不符时，MUST 整批拒绝并返回 409，响应中 MUST 列出冲突条目的标识、server 端当前 revision、当前删除状态、密文大小与 `updated_at`，MUST NOT 部分写入，MUST NOT 在 409 响应中返回冲突条目密文。

#### Scenario: 无冲突推送

- **WHEN** 批量条目的 base_revision 均与 server 当前值一致
- **THEN** 全部写入，每条获得新的单调递增 revision，响应返回新 revision 集合

#### Scenario: 冲突整批拒绝

- **WHEN** 批量中任一 entry 的 base_revision 落后于 server 当前 revision
- **THEN** 返回 409 与冲突清单及非机密描述符，整批无任何写入且响应不含 ciphertext

### Requirement: 增量拉取

拉取接口 SHALL 接受 `since` 参数，返回该 vault 中 revision 大于 since 的全部条目（含删除标记）、每条最近更新的 `updated_at`，以及当前最新 revision。空增量 SHALL 返回空列表与最新 revision，不报错。

#### Scenario: 增量拉取含删除

- **WHEN** 某 entry 在上次同步后被删除
- **THEN** 拉取结果包含该 entry 的删除标记条目及其更新时间

#### Scenario: 无更新

- **WHEN** since 等于当前最新 revision
- **THEN** 返回空条目列表与相同 latest revision
