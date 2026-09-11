## ADDED Requirements

### Requirement: History Tab 延迟加载

TUI 启动 SHALL NOT 发起 History 查询；History 数据 SHALL 在用户首次激活 History Tab 时查询并缓存。激活后的刷新语义保持既有行为（server 模式提供、手动刷新可用；git 模式或 server 不可用时优雅降级为无数据/空态）。

#### Scenario: 启动不查 History

- **WHEN** server 模式下启动 TUI 且用户停留在 env Tab
- **THEN** 进程未发起任何 History 请求，其余 Tab 行为不变

#### Scenario: 首次激活 History Tab

- **WHEN** 用户首次切换到 History Tab
- **THEN** 发起一次查询并在加载完成后展示；再次激活时复用缓存，手动刷新才重新查询
