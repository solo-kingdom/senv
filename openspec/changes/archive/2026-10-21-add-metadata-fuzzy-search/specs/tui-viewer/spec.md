# Spec Delta

## MODIFIED Requirements

### Requirement: 全局跨类型搜索

TUI SHALL 提供全局搜索 overlay（触发键 `S`），跨 Env/Text/Config/SSH/LLM/MCP/Backup 数据搜索。搜索 MUST 只匹配非秘密标识与说明字段，与 CLI/MCP `metadata-search` 对齐：env/text/backup 的 group/key/条目 description/组 description；config 的 group/name/description/target path；host alias/hostname/group/tags/description；provider alias/description；MCP 档案 alias/command/description；backup 的 group/key/description。绝不匹配值、私钥内容、凭据或 MCP env 值。搜索结果 MUST 标识条目类型，并支持跳转定位（SSH/LLM/MCP/Backup 结果跳转到对应 Tab 并定位光标）。空输入时 overlay 可展示全量 inventory（与 CLI 强制 query 不同，属人机浏览）；一旦有输入，匹配规则与 CLI/MCP 一致（大小写不敏感；多 token AND）。

#### Scenario: 触发全局搜索
- **WHEN** 用户按 `S` 键
- **THEN** 弹出全局搜索 overlay，含输入框和跨类型结果列表

#### Scenario: 搜索结果按类型展示
- **WHEN** 用户输入 `database` 且数据中存在匹配的 env key、text key、config name
- **THEN** 结果列表显示所有匹配项，每项标注类型（Env/Text/Cfg）、分组（若适用）、key/name，值部分遮蔽（env 显示 `***`，text 显示 size，config 显示 target）

#### Scenario: description 可命中
- **WHEN** 用户输入某串仅出现在某 env 条目 description 中
- **THEN** 该 env 条目出现在结果中

#### Scenario: SSH 与 LLM 结果
- **WHEN** 用户输入某 host alias 或 provider alias 的前缀
- **THEN** 结果显示对应 SSH host 或 LLM provider 条目，`enter` 后跳转到对应 Tab 并选中该条目

#### Scenario: MCP 档案结果
- **WHEN** 用户输入某 MCP Server 档案 alias 或 command 的前缀
- **THEN** 结果显示对应 MCP 条目，`enter` 后跳转到 MCP Tab 并选中该档案

#### Scenario: 搜索不匹配值
- **WHEN** 用户输入某个仅出现在值中而不在任何可匹配字段中的字符串
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
