## MODIFIED Requirements

### Requirement: 说明附着面

说明 SHALL 可贴在 env 条目、text 条目、backup 条目、config 条目、Host、KeyPair、LLM Provider 档案、MCP Server 档案，以及 env 分组、text 分组与 backup 分组上。说明 MUST NOT 贴在本机状态（当前指向、导出状态）。LLM 模型目录/模型元数据中的文案 MUST NOT 当作本说明。OpenSSH 私钥 comment MUST NOT 当作本说明。

#### Scenario: 给 env 条目写说明

- **WHEN** 用户为已存在的 env 条目设置说明
- **THEN** 该说明随条目持久化，后续 list/get 元信息可见，值不变

#### Scenario: Provider 说明与模型文案分离

- **WHEN** 用户为 LLM Provider 档案写入说明，且某模型已有目录简介
- **THEN** 档案说明与模型简介彼此独立，互不覆盖

#### Scenario: 给 backup 条目写说明

- **WHEN** 用户为已存在的 backup 条目设置说明
- **THEN** 该说明随条目持久化，后续 list 元信息可见，value 不变

### Requirement: list 必带说明

env/text/backup/config 的条目 list、env/text/backup 的 group list、Host/KeyPair list、LLM Provider list、MCP Server list SHALL 包含说明（可为空）。MCP 的 `ssh_host_list`、`llm_provider_list`、`mcp_server_list` SHALL 同样返回说明。backup list MUST NOT 输出 value。说明不是密钥值，但客户端 MUST NOT 把说明当作可在对话里随意复述的秘密。

#### Scenario: group list 展示空说明

- **WHEN** 某 env 组无说明
- **THEN** group list 仍列出该组，说明为空

#### Scenario: MCP env list 并列返回说明

- **WHEN** agent 调用 `senv_env_list`
- **THEN** 结果含各条目说明；值暴露面保持现状（完整值），不因本能力缩小或扩大

#### Scenario: backup list 展示说明

- **WHEN** 条目 `notes:DUMP` 有说明「冷备份」
- **THEN** `senv backup list notes` 行含该说明，不含正文
