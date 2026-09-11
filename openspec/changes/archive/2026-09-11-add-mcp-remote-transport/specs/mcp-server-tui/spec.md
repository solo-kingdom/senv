## MODIFIED Requirements

### Requirement: 档案写操作

MCP Tab SHALL 提供档案写操作：`n` 新建、`e` 编辑选中档案、`d` 删除。新建表单 SHALL 收集别名、传输类型（`stdio` / `http` / `sse`）、description，并按传输收集对应字段：`stdio` 收集 command（必填）、args、env；`http` / `sse` 收集 url（必填）与 headers。选择 remote 传输时表单 MUST NOT 出现 command / args / env 字段。编辑表单 SHALL 使别名只读，MUST NOT 提供重命名键，且 SHALL 允许切换传输类型并按目标传输重新校验字段。args SHALL 以一行一个参数经 `$EDITOR` 编辑并保序；env SHALL 以一行 `KEY=VALUE` 经 `$EDITOR` 编辑，值按模板原样存储；headers SHALL 以一行 `Name: Value` 经 `$EDITOR` 编辑，值按模板原样存储。删除 SHALL 二次确认；若本机台账显示该档案已导出，确认文案 SHALL 列出相关 agent id，并说明删除不撤回已导出条目。写操作 SHALL 调用既有档案管理接口，成功后刷新左栏；失败 SHALL 经统一提示条回显且不改变既有档案。

#### Scenario: TUI 新建档案

- **WHEN** 用户按 `n` 填写别名 github、command `npx`、一行参数后提交
- **THEN** 档案保存成功，左栏出现 github，提示成功

#### Scenario: TUI 新建 remote 档案

- **WHEN** 用户按 `n` 选择传输 `http`、填写别名 web-reader 与 url 后提交
- **THEN** 档案保存成功，传输为 `http`，表单中未出现 command / args / env 字段

#### Scenario: 缺少 command 拒绝

- **WHEN** 用户提交新建表单但 command 为空
- **THEN** 表单内联报错并保持打开，不创建档案

#### Scenario: remote 缺少 url 拒绝

- **WHEN** 用户提交新建表单选择 `http` 但 url 为空
- **THEN** 表单内联报错并保持打开，不创建档案

#### Scenario: 编辑不改别名

- **WHEN** 用户对 github 按 `e` 修改 command 后提交
- **THEN** 档案更新，别名仍为 github

#### Scenario: 删除不撤回

- **WHEN** github 已导出到 cursor，用户按 `d` 并确认
- **THEN** vault 中档案被删除，cursor 全局配置中的已导出条目保持不变，确认文案曾列出 cursor

### Requirement: env 值可见性

MCP Tab 的列表、详情弹层、计划页与 toast MUST NOT 渲染 `env` 字面量、header 值或 `url` 的 query；列表与详情 MAY 展示 `env` 键名、引用模板原文与 `url` 的 `scheme://host` 来源。用户在档案表单中显式打开 `$EDITOR` 编辑 env 或 headers 字段时，编辑器是 TUI 内唯一允许出现对应字面量的解密面；主表单视图 MUST 只显示键名或键数量。

#### Scenario: 列表不泄漏值

- **WHEN** 档案 github 的 env 含字面量 token
- **THEN** 左栏与详情只出现键名，不出现该 token

#### Scenario: remote 详情不泄漏值

- **WHEN** 档案 web-reader 的 url 含 query 凭证且 headers 含 Bearer token
- **THEN** 左栏与详情只显示 `scheme://host` 来源与 header 键名，不出现 query 与 header 值

#### Scenario: 计划不渲染解析值

- **WHEN** 导出计划含将写入明文 env 或 headers 的条目
- **THEN** 计划页标注「明文」与目标路径，不显示解析后的值

#### Scenario: 编辑器是解密面

- **WHEN** 用户在编辑表单中打开 env 的 `$EDITOR`
- **THEN** 编辑器缓冲区含 `KEY=VALUE` 行；关闭编辑器回到表单后，表单主视图仍只显示键名
