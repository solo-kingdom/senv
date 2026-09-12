# tui-viewer 增量

## ADDED Requirements

### Requirement: Tab 加载态

TUI 的每个 Tab SHALL 区分加载态与空态：数据装载完成前，Tab SHALL 在常驻面板几何内显示加载提示（与既有 Tab 的 `loading groups…` 同风格、同界面语言），MUST NOT 显示携带操作指引的空态文案（如 "no LLM provider profiles yet; … press n to create"）；空态文案与操作指引 SHALL 仅在装载完成后、数据集确实为空时出现。加载提示的呈现方式与 env/text/config 既有范式一致。错误态（装载失败）SHALL 展示失败原因，与加载态、空态区分。

#### Scenario: AI Tab 装载期间显示加载态

- **WHEN** 用户切换到 AI Tab 且 provider 数据尚未装载完成
- **THEN** 内容区渲染常驻面板几何，框内显示加载提示，不出现 "no LLM provider profiles yet" 等空态指引

#### Scenario: MCP Tab 装载期间显示加载态

- **WHEN** 用户切换到 MCP Tab 且 server 档案数据尚未装载完成
- **THEN** 内容区渲染常驻面板几何，框内显示加载提示，不出现 "no MCP server profiles yet" 等空态指引

#### Scenario: SSH Tab 装载期间显示加载态

- **WHEN** 用户切换到 SSH Tab 且 host/keypair 数据尚未装载完成
- **THEN** 内容区渲染常驻面板几何，框内显示加载提示，不出现 "no SSH assets yet" 等空态指引

#### Scenario: 装载完成后空态恢复指引

- **WHEN** AI/MCP/SSH Tab 数据装载完成且数据集为空
- **THEN** 显示既有空态文案与操作指引（如 "press n to create"），布局与装载期间一致、无跳动

#### Scenario: History Tab 装载期间显示加载态

- **WHEN** 用户首次激活 History Tab、查询尚未返回
- **THEN** 内容区渲染常驻面板几何并在框内显示加载提示，而非无框裸文本

#### Scenario: Audit Tab 装载期间显示加载态

- **WHEN** Audit Tab 数据尚未装载完成
- **THEN** 内容区渲染常驻面板几何并在框内显示加载提示，而非无框裸文本

### Requirement: History 与 Audit 面板几何

History Tab 与 Audit Tab 的内容面板 SHALL 撑满内容区可用宽高（与其他 Tab 的外层几何一致），并在终端尺寸变化时跟随重排。列表 SHALL 采用与其他 Tab 一致的窗口化呈现：可见行数受面板高度约束、光标始终可见，列表超出可见范围时标题 SHALL 显示当前可见区间（如「4–12」）；行内容超宽时 SHALL 以 `…` 截断，MUST NOT 撑破外框。空态、错误态与恢复确认等附加信息 SHALL 呈现在面板内部。

#### Scenario: 面板撑满内容区

- **WHEN** 终端为正常可用尺寸（如 80×24），用户切换到 History Tab 或 Audit Tab
- **THEN** 面板边框占满内容区宽高，与其他 Tab 的面板外框视觉一致，不随行数或行宽伸缩

#### Scenario: resize 跟随重排

- **WHEN** 用户在 History Tab 或 Audit Tab 停留时调整终端宽度或高度
- **THEN** 面板边框跟随新尺寸重排，与其他 Tab 行为一致

#### Scenario: 列表窗口化与区间提示

- **WHEN** History Tab 版本数超过面板可视行数且光标移出当前窗口
- **THEN** 列表滚动跟随光标，标题显示当前可见区间（如「4–12」）

#### Scenario: 附加信息位于面板内部

- **WHEN** History Tab 进入恢复确认或显示 flash 提示，或 Audit Tab 显示过滤输入行
- **THEN** 该信息渲染在面板边框内部，面板总高不超出内容区
