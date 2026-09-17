## ADDED Requirements

### Requirement: config 说明超限在表单内失败
TUI 创建或改 meta 时，说明超过 2048 字节 SHALL 在表单内报错，MUST NOT 写入。

#### Scenario: TUI meta 超长说明
- **WHEN** 用户在 config 元信息表单提交超长 description
- **THEN** 提交失败，表单保留输入，索引不变
