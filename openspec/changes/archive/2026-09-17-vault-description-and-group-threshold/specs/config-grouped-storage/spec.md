## ADDED Requirements

### Requirement: config 说明上限
config 条目已有的 description SHALL 遵守 2048 字节上限；超限的 create 或 SetMeta MUST 拒绝且不改索引。list/get 已展示 description 的行为保持不变。

#### Scenario: oversize config description
- **WHEN** 用户以超过 2048 字节的 description 创建或修改 config
- **THEN** 操作失败，不写入该 description
