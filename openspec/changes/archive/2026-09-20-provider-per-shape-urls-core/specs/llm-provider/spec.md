## ADDED Requirements

### Requirement: Provider 形态地址

LLM Provider SHALL 支持三个可选形态地址字段 `chat_base_url` / `responses_base_url` / `anthropic_base_url`，分别声明 openai-chat、openai-responses 与 anthropic 接口形态的接入地址；空值表示未声明。`senv ai provider add/edit` SHALL 通过重复 flag `--shape-url <api_shape>=<url>` 设置（`api_shape` 取值同 `--api-shape` 的合法枚举）：`add` 出现即设置；`edit` 传值设置、空值（`--shape-url <api_shape>=`）清空该字段、未出现的 key 保留原值。非法 key MUST 以非 0 退出且不写入。三字段的 URL 校验 SHALL 与 `BaseURL` 一致：默认仅接受 HTTPS、显式 `--allow-http` 后接受 HTTP、拒绝空 host 与 userinfo。归一化 SHALL 分族执行：`chat_base_url` 与 `responses_base_url` 沿用 `BaseURL` 的 OpenAI 兼容归一（收敛尾斜杠、补末段版本段、纯数字版本段视为已归一）；`anthropic_base_url` 原样存储、仅收敛尾斜杠，MUST NOT 追加、剥离或改写任何路径段（包括 `/v1`）。`BaseURL` SHALL 保持必填，语义为默认地址与推断源。存量档案 MUST NOT 要求迁移：字段缺省即旧行为。`show`/`list` SHALL 展示三个形态地址（未设显示 `-`），MUST NOT 输出凭据明文。

#### Scenario: 设置形态地址

- **WHEN** 用户执行 `senv ai provider add gw --base-url https://gw.example.com/v1 --shape-url anthropic=https://gw.example.com/api/anthropic ...`（其余必填项合法）
- **THEN** 档案保存 `anthropic_base_url` 为 `https://gw.example.com/api/anthropic`，`show` 展示该形态地址

#### Scenario: anthropic 形态地址原样存储

- **WHEN** 用户提供 `--shape-url anthropic=https://gw.example.com/api/anthropic/`（尾斜杠）或 `--shape-url anthropic=https://gw.example.com/api/anthropic/v1`
- **THEN** 前者收敛尾斜杠后存储；后者原样存储，MUST NOT 被剥去 `/v1` 或追加版本段

#### Scenario: OpenAI 族形态地址沿用归一

- **WHEN** 用户提供 `--shape-url responses=https://gw.example.com/responses-root`
- **THEN** 档案保存 `https://gw.example.com/responses-root/v1`，命令提示归一改写

#### Scenario: 非法 key 拒绝

- **WHEN** 用户传入 `--shape-url openai=https://gw.example.com`
- **THEN** 命令以非 0 退出并列出合法 key（openai-chat / openai-responses / anthropic），不写入任何变更

#### Scenario: edit 空值清空、省略保留

- **WHEN** 档案已设 `chat_base_url` 与 `anthropic_base_url`，用户执行 `edit <alias> --shape-url chat=` 且未出现 anthropic key
- **THEN** `chat_base_url` 从档案移除，`anthropic_base_url` 保持不变

#### Scenario: 校验与 HTTP 门禁覆盖形态地址

- **WHEN** 用户传入 `--shape-url anthropic=http://intranet.example.com/anthropic` 且未提供 `--allow-http`，或传入含 userinfo 的形态地址
- **THEN** 命令以非 0 退出，不写档案或凭据

#### Scenario: show/list 展示形态地址

- **WHEN** 档案设置了部分形态地址且用户执行 show 或 list
- **THEN** 输出包含已设形态地址，未设项显示 `-`，不含凭据明文
