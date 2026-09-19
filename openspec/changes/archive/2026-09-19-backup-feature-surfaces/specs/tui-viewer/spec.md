## ADDED Requirements

### Requirement: Backup Tab 浏览与操作

TUI SHALL 提供独立 Backup Tab，采用与 Text Tab 相同的双栏分组侧栏布局（All 伪组默认选中，空分组计数 0，行前缀 `group/key`）。右侧列表 MUST 仅显示 key、大小、更新时间与说明，MUST NOT 显示 value。Backup Tab MUST 支持浏览、新建（vim）、vim 编辑、重命名 key、删除、导入、导出、新建/重命名/删除分组、Tab 内过滤与多选批量删除/导出。MUST NOT 提供解引用切换。`default` 分组 MUST NOT 可改名或删除。

#### Scenario: 默认全览

- **WHEN** 打开 Backup Tab
- **THEN** 左侧 All 默认选中，右侧显示全部 backup 条目且带 `group/key` 前缀，不显示正文

#### Scenario: vim 编辑 backup

- **WHEN** 用户选中某 backup 条目按 `e`
- **THEN** TUI 挂起并打开编辑器预填现有内容，保存后重新加密并刷新列表

#### Scenario: default 组受保护

- **WHEN** 用户在 Backup Tab 侧栏选中 `default` 并尝试重命名或删除
- **THEN** 操作被拒绝，组仍在

## MODIFIED Requirements

### Requirement: 全局跨类型搜索

TUI SHALL 提供全局搜索 overlay（触发键 `S`），跨 Env/Text/Config/SSH/AI/MCP/Backup 数据搜索。搜索 MUST 只匹配标识字段（key/name、host alias/hostname、provider alias、MCP 档案 alias/command、backup 的 group/key/description），绝不匹配值、私钥内容、凭据或 MCP env 值。搜索结果 MUST 标识条目类型，并支持跳转定位（SSH/AI/MCP/Backup 结果跳转到对应 Tab 并定位光标）。

#### Scenario: 触发全局搜索

- **WHEN** 用户按 `S` 键
- **THEN** 弹出全局搜索 overlay，含输入框和跨类型结果列表

#### Scenario: 搜索结果按类型展示

- **WHEN** 用户输入 `database` 且数据中存在匹配的 env key、text key、config name
- **THEN** 结果列表显示所有匹配项，每项标注类型（Env/Text/Cfg）、分组（若适用）、key/name，值部分遮蔽（env 显示 `***`，text 显示 size，config 显示 target）

#### Scenario: SSH 与 AI 结果

- **WHEN** 用户输入某 host alias 或 provider alias 的前缀
- **THEN** 结果显示对应 SSH host 或 LLM provider 条目，`enter` 后跳转到对应 Tab 并选中该条目

#### Scenario: MCP 档案结果

- **WHEN** 用户输入某 MCP Server 档案 alias 或 command 的前缀
- **THEN** 结果显示对应 MCP 条目，`enter` 后跳转到 MCP Tab 并选中该档案

#### Scenario: 搜索不匹配值

- **WHEN** 用户输入某个仅出现在值中而不在任何 key/name 中的字符串
- **THEN** 结果列表为空（显示"无匹配"），不返回任何值匹配

#### Scenario: 搜索不返回秘密

- **WHEN** 用户输入某个仅出现在 SSH 私钥内容、LLM 凭据或 MCP env 值中的字符串
- **THEN** 结果列表为空（显示"无匹配"）

#### Scenario: 跳转定位

- **WHEN** 用户在搜索结果选中某条按 `enter`
- **THEN** overlay 关闭，切换到该条目所属的 Tab，选中对应分组（若有）和条目

#### Scenario: 关闭搜索

- **WHEN** 用户按 `esc`
- **THEN** overlay 关闭，返回之前的 Tab 视图

#### Scenario: 搜索 backup 标识

- **WHEN** 用户按 `S` 并输入某 backup key 或说明片段
- **THEN** 结果含对应 Backup 条目，`enter` 跳转 Backup Tab 并定位；正文即使含相同片段 MUST NOT 作为匹配依据
