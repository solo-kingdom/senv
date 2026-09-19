# 0027-provider-per-shape-urls

LLM Provider 档案在保留单一 `BaseURL`（必填，语义收窄为默认地址 + 推断源）的基础上，新增三个可选形态地址字段 `chat_base_url` / `responses_base_url` / `anthropic_base_url`，分别为 openai-chat、openai-responses 与 anthropic 接口形态声明独立接入地址；某形态地址未设置时，沿用 ADR-0004 规则从 `BaseURL` 自动推断。`senv ai switch` 门禁放宽：目标协议族存在显式形态地址即放行（不受 `api_shape` 声明影响），无显式地址时维持声明判据，拒绝文案给出「改档案形态 / 补配该族 `--shape-url` / 换 provider」三个动作。实际写入地址跟随已解析线协议：形态地址的存在性只参与门禁，从不反向决定 OpenAI 族内的 chat / responses 选择。

归一化分族：`chat_base_url` 与 `responses_base_url` 沿用 `BaseURL` 的 OpenAI 兼容归一（补末段版本段、收敛尾斜杠）；`anthropic_base_url` 原样存储、仅收敛尾斜杠——它存的是 claude-code 将拼 `+ /v1/messages` 的 root，`/api/anthropic` 这类路径前缀不可被版本段规则触碰，传带 `/v1` 的值不被修正，由 `switch` 输出实际写入地址与来源（显式字段名 / 由 BaseURL 推断）让其可见。CLI 用重复 flag `--shape-url <api_shape>=<url>`（传值设置、空值清空、edit 省略保留）；TUI provider 表单渲染三个固定字段；MCP `llm_provider_list` 白名单视图新增 `api_shape` 与 `shape_urls`（地址按 `base_url` 先例视为非密钥）。存量档案不迁移：字段缺省即旧行为。

## Considered Options

- **`shape_urls` map（key 为 api_shape 值）**：扩展性冗余——shapes 是封闭三元枚举，map 让 CLI flag、TUI 表单、校验与展示都变绕。放弃，用平铺字段。
- **去掉 `BaseURL`、三字段全显式**：破坏存量档案与「同一份 provider 在多机多工具间可比性」。放弃。
- **per-agent 地址覆盖**（ADR-0004 两次推迟的逃生舱原形）：agent 矩阵继续不进 vault schema；本决策把逃生舱粒度开到 per-shape 即止。
- **地址存在性牵引线协议**（只配了一个 OpenAI 族地址时让它的存在决定 chat/responses）：对用户更直觉，但引入「从地址存在性推断」，违背 ADR-0006 把猜测挡在安全路径之外的原则。放弃，用 `switch` 输出地址来源补偿。

## Consequences

- 部分取代 ADR-0006：「形态只由 `api_shape` 声明决定」不再成立——显式形态地址的存在本身即「该族被服务」的声明，`api_shape` 的职责收敛为 OpenAI 族内选线协议 + 无显式地址时的兼容判据。
- 细化 ADR-0004：「一份档案一个接入地址」收窄为「一份档案一个默认地址 + 可选形态地址」；推断路径（未设形态地址时的行为）与 ADR-0004 完全一致，存量档案零迁移。
- `anthropic_base_url` 透传可能存下带 `/v1` 的值，claude-code 会拼出 `/v1/v1/messages` 死地址：接受——不做隐式改写（同 ADR-0004 对 `/v1beta` 的取舍），switch 输出与文档约定（「anthropic 地址填 root」）补偿。
- 加密 blob 内的 schema 扩展：sync 通道与 server 白名单无需变更。

## Status

采纳（2026-09-20）。修订 [ADR-0006](./0006-provider-api-shape.md) 的兼容判定语义，细化 [ADR-0004](./0004-single-base-url-per-protocol-family.md) 的单地址原则；归一规则本身不变。
