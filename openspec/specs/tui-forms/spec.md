# tui-forms Specification

## Purpose
为 TUI 的多字段编辑提供统一的结构化表单：按字段顺序渲染、内联校验、遮蔽与引用选择等字段类型，并复用既有编辑器闭环处理长文本，使 SSH host、LLM provider、MCP Server 档案、config 元信息与分组重命名等编辑界面共享同一契约。

## Requirements

### Requirement: 结构化表单契约

TUI SHALL 提供可复用的结构化表单，用于多字段编辑（SSH host、LLM provider、MCP Server 档案、config 元信息、分组重命名等）。表单 SHALL 按字段顺序渲染，支持 `tab`/`shift+tab` 与上下方向键在字段间移动、`enter` 提交、`esc` 取消。提交前 SHALL 逐字段校验并在字段旁内联展示错误，校验失败时 MUST 保持表单打开且不丢失已填内容；取消 MUST 不产生任何写入副作用。表单打开期间全局快捷键（数字、`q`、`?`、`S`）MUST NOT 生效。

#### Scenario: 字段遍历与提交
- **WHEN** 用户打开一个含多个字段的表单并依次按 `tab` 移动到末字段后按 `enter`
- **THEN** 表单校验通过并提交，调用对应的 Manager 方法，界面回到列表并刷新

#### Scenario: 校验失败不丢输入
- **WHEN** 用户提交的表单中某字段非法（如端口非数字、alias 重复）
- **THEN** 表单保持打开，错误显示在该字段旁，其它字段内容不变，不发生写入

#### Scenario: 取消无副作用
- **WHEN** 用户在表单中按 `esc`
- **THEN** 表单关闭，不调用任何 Manager 方法，列表与数据不变

#### Scenario: 输入模式隔离全局键
- **WHEN** 表单处于打开状态
- **THEN** 数字键、`q`、`?`、`S` 作为字段输入或表单内操作，不触发 Tab 切换、退出或 overlay

### Requirement: 字段类型

表单 SHALL 支持以下字段类型：单行文本、遮蔽输入（secret，回显为掩码且不进任何渲染文本）、枚举选择（在候选集内选择）、引用选择（从既有条目中挑选，如 vault 的 env/text 条目或 keypair）、路径输入。枚举与引用选择字段 MUST 支持「无/空」选项。

#### Scenario: 遮蔽输入
- **WHEN** 用户在 secret 字段输入凭据
- **THEN** 输入内容以掩码回显，表单与后续提示文本中不出现明文

#### Scenario: 引用选择
- **WHEN** 用户为某字段选择既有条目
- **THEN** 以选择器列出候选并返回选中标识，不复制其内容

### Requirement: 长文本走编辑器闭环

对多行内容与自由属性（SSH host 的 `extra`、MCP Server 档案的 `args` 与 `env`），表单 SHALL 提供跳转到 `$EDITOR` 的入口，复用既有「临时文件 600 → 编辑 → 读回 → 清理」闭环，MUST NOT 新增加密或临时文件路径。

#### Scenario: 自由属性编辑
- **WHEN** 用户在 host 表单中对 `extra` 字段触发编辑器
- **THEN** TUI 挂起并打开 `$EDITOR`，退出后读回内容填入该字段，临时文件被删除

#### Scenario: MCP args 与 env 走编辑器
- **WHEN** 用户在 MCP 档案表单中对 args 或 env 字段触发编辑器
- **THEN** TUI 挂起并打开 `$EDITOR`，退出后读回内容填入该字段，临时文件被删除
