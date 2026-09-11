# config-tui 增量

## ADDED Requirements

### Requirement: 创建配置走结构化表单

config tab 的 `n` 创建 SHALL 使用结构化表单一次收集：name（必填，重名冲突内联报错）、源文件路径（必填，存在性校验）、target 路径（必填）、分组（从既有分组选择，可空 = default）、描述（可选）。表单契约（`tab`/`shift+tab` 导航、内联校验不丢输入、`esc` 取消零副作用、输入模式隔离全局键）遵循 tui-forms 能力规约；提交失败 SHALL 经 reopen 模式回填表单修正，MUST NOT 落盘部分状态。

#### Scenario: 表单创建成功
- **WHEN** 用户按 `n` 填写 name、源文件路径、target 路径、分组与描述后提交
- **THEN** 调用 `config.Manager.Create` 加密导入，列表刷新并出现新条目

#### Scenario: 必填缺失内联报错
- **WHEN** 用户未填源文件路径直接提交
- **THEN** 该字段旁内联报错，表单保持打开且已填内容不丢失，不发生写入

#### Scenario: 取消零副作用
- **WHEN** 用户在创建表单按 `esc`
- **THEN** 表单关闭，不创建条目、不读源文件，列表不变
