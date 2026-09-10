## Purpose

把 LLM provider 档案与 agent 指针以只读 MCP 工具暴露给 AI agent：响应是显式白名单视图（不含凭据明文），agent 可据此回答「有哪些 provider、某个 agent 现在指向谁」。

## ADDED Requirements

### Requirement: 列出 LLM provider 档案
`llm_provider_list` 工具 SHALL 返回全部 provider 档案的 JSON 数组，字段白名单为 alias、base_url、credential_ref、catalog_provider、default_model、models（模型 id 列表）、created_at、updated_at。响应 MUST NOT 包含任何凭据明文。工具需要有效 MCP session（与其他受保护工具一致）。

#### Scenario: 列出档案
- **WHEN** vault 中存在档案 main 且 agent 调用 llm_provider_list
- **THEN** 返回含 main 档案白名单字段的 JSON，无任何 key 明文字段或值

#### Scenario: 无档案
- **WHEN** vault 中无 provider 档案
- **THEN** 返回空数组而非错误

#### Scenario: session 无效
- **WHEN** MCP session 未启动或已过期
- **THEN** 工具返回错误提示先执行 `senv session start`

### Requirement: 查询各 agent 当前指向
`llm_agent_status` 工具 SHALL 返回全部已知 agent（受支持与不支持）的 JSON 数组，字段为 agent、name、supported、pointer（provider/model/switched_at，未切换为 null）、config_path。指针是本机状态，该工具 MUST NOT 触发任何写入。

#### Scenario: 混合状态
- **WHEN** claude-code 已切换、opencode 未切换且 agent 调用 llm_agent_status
- **THEN** 返回的数组中三态齐全：已切换含 pointer 对象、未切换 pointer 为 null、cursor/zcode supported 为 false

### Requirement: 只读边界
LLM 相关 MCP 工具 SHALL 仅为上述两个查询；MUST NOT 提供 provider 增删改或 agent 切换工具。工具目录（list-tools）与实际注册保持一致。

#### Scenario: 目录一致
- **WHEN** 执行 `senv mcp list-tools`
- **THEN** 输出包含 llm_provider_list 与 llm_agent_status，且不含任何 LLM 写入类工具
