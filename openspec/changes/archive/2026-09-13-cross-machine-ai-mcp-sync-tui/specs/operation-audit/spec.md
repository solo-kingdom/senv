## MODIFIED Requirements

### Requirement: tui 查看

TUI SHALL 提供审计 Tab：按时间新到旧浏览本机事件（含日期时间、操作、目标、结果），支持操作类型过滤、自由文本过滤与翻页；无事件时明确提示。审计 Tab SHALL 另提供"自上次 pull"过滤视图，仅显示最近一次成功 pull 之后发生的事件；从未 pull 过时该视图显示明确提示而非报错。

#### Scenario: tui 浏览审计

- **WHEN** 用户切换到审计 Tab
- **THEN** 按时间新到旧展示事件列表，含日期时间与结果，可翻页

#### Scenario: 自由文本过滤

- **WHEN** 用户在审计 Tab 输入过滤串
- **THEN** 仅显示目标或操作类型包含该串的事件，且可清除过滤回到全量

#### Scenario: 自上次 pull 过滤

- **WHEN** 用户在审计 Tab 切换到"自上次 pull"视图，且本机存在上次 pull 时间
- **THEN** 仅显示该时间之后的事件，pull 引入/覆盖档案的同步与写操作事件可见，后台自动 pull 完成后列表刷新

#### Scenario: 从未 pull 过

- **WHEN** 本机从未执行过 pull 时切换到"自上次 pull"视图
- **THEN** 显示明确的空态提示，不报错
