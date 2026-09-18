## Purpose
定义配置源条目与 env/text 分组上「说明」字段的可见行为：谁可以贴、多长、何时必填、如何被 list 看见。

## ADDED Requirements

### Requirement: 说明附着面
说明 SHALL 可贴在 env 条目、text 条目、config 条目、Host、KeyPair、LLM Provider 档案、MCP Server 档案，以及 env 分组与 text 分组上。说明 MUST NOT 贴在本机状态（当前指向、导出状态）。LLM 模型目录/模型元数据中的文案 MUST NOT 当作本说明。OpenSSH 私钥 comment MUST NOT 当作本说明。

#### Scenario: 给 env 条目写说明
- **WHEN** 用户为已存在的 env 条目设置说明
- **THEN** 该说明随条目持久化，后续 list/get 元信息可见，值不变

#### Scenario: Provider 说明与模型文案分离
- **WHEN** 用户为 LLM Provider 档案写入说明，且某模型已有目录简介
- **THEN** 档案说明与模型简介彼此独立，互不覆盖

### Requirement: 说明形态与上限
说明 SHALL 为单段纯文本。长度 MUST 不超过 2048 字节（UTF-8）。超过上限的写入 MUST 被拒绝且不改原值。空说明（缺省或显式清空）对存量记录合法。说明 MUST NOT 被当作 Markdown 渲染。

#### Scenario: 超长说明被拒绝
- **WHEN** 写入说明超过 2048 字节
- **THEN** 操作失败，原说明与值均不变

#### Scenario: 空说明合法
- **WHEN** 读取没有说明字段的旧记录
- **THEN** 视为空说明，不报错、不强制回填

### Requirement: list 必带说明
env/text/config 的条目 list、env/text 的 group list、Host/KeyPair list、LLM Provider list、MCP Server list SHALL 包含说明（可为空）。MCP 的 `ssh_host_list`、`llm_provider_list`、`mcp_server_list` SHALL 同样返回说明。说明不是密钥值，但客户端 MUST NOT 把说明当作可在对话里随意复述的秘密。

#### Scenario: group list 展示空说明
- **WHEN** 某 env 组无说明
- **THEN** group list 仍列出该组，说明为空

#### Scenario: MCP env list 并列返回说明
- **WHEN** agent 调用 `senv_env_list`
- **THEN** 结果含各条目说明；值暴露面保持现状（完整值），不因本能力缩小或扩大

### Requirement: 条目说明可选
新建或更新 env/text/config/Host/KeyPair/LLM Provider/MCP Server 条目或档案时，说明 MAY 省略。省略 MUST 写空说明，MUST NOT 拒绝该写入（组新建除外，见 group-threshold）。

#### Scenario: 不带说明创建 Host
- **WHEN** 用户 `senv host add` 不传说明
- **THEN** Host 创建成功，说明为空

### Requirement: Host LLM MCP 说明写入面
Host、KeyPair、LLM Provider、MCP Server 的说明 SHALL 可通过 CLI 与 TUI 读写。MCP 工具对这三类 MUST 保持只读：可在 list/get 中返回说明，MUST NOT 提供只改说明的写工具。

#### Scenario: MCP 不能改 Host 说明
- **WHEN** agent 仅通过 MCP 工具访问
- **THEN** 不存在可修改 Host/KeyPair/LLM Provider/MCP Server 说明的工具
