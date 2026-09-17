## MODIFIED Requirements

### Requirement: 列出 LLM provider 档案
`llm_provider_list` 工具 SHALL 返回全部 provider 档案的 JSON 数组，字段白名单为 alias、base_url、credential_ref、catalog_provider、default_model、models（模型 id 列表）、description、created_at、updated_at。响应 MUST NOT 包含任何凭据明文。工具需要有效 MCP session（与其他受保护工具一致）。

#### Scenario: 列出档案
- **WHEN** vault 中存在档案 main 且 agent 调用 llm_provider_list
- **THEN** 返回含 main 档案白名单字段（含 description，可为空）的 JSON，无任何 key 明文字段或值

#### Scenario: 无档案
- **WHEN** vault 中无 provider 档案
- **THEN** 返回空数组而非错误

#### Scenario: session 无效
- **WHEN** MCP session 未启动或已过期
- **THEN** 工具返回错误提示先执行 `senv session start`
