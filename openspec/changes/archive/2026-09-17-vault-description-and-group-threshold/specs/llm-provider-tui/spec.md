## ADDED Requirements

### Requirement: AI Tab 可编辑档案说明
新建与编辑 Provider 表单 SHALL 包含说明字段（可选）。详情 SHALL 展示档案说明。说明超限时 SHALL 留在表单内修正，不写档案。

#### Scenario: TUI 新建含说明
- **WHEN** 用户在 AI Tab 新建档案并填写说明后提交成功
- **THEN** 档案保存该说明，详情可见

#### Scenario: TUI 清空说明
- **WHEN** 用户编辑表单清空说明并提交
- **THEN** 档案说明变为空，其他字段不受影响
