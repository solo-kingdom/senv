# Glossary — provider-per-shape-urls

- **形态地址（per-shape URL）**：provider 档案中为特定接口形态显式配置的接入地址，对应 schema 字段 `chat_base_url` / `responses_base_url` / `anthropic_base_url`（均可选）。
- **推断地址（inferred URL）**：形态地址未设置时，从 `BaseURL` 按 ADR-0004 规则推导出的地址（OpenAI 族保持带版本段形态；Anthropic 族剥末段 `/v1`）。
- **推断源（`BaseURL`）**：既有单地址字段，语义收窄为「默认地址 + 推断源」，仍必填，不做 per-agent 语义。
- **服务族（served family）**：目标 agent 协议族在档案上存在显式形态地址即视为「该族被服务」，是 switch 门禁的放行条件之一。
- **线协议（wire protocol, W）**：OpenAI 族内 chat vs responses 的实际接口协议。由 `api_shape`（openai-* 声明）或 agent 既有默认决定；**形态地址跟随 W 选择，从不反向牵引 W**。
- **`api_shape`**：既有声明字段（ADR-0006）。职责收敛为：OpenAI 族内选线协议；目标族无显式形态地址时的兼容判据。
- **`--shape-url <api_shape>=<url>`**：CLI 读写形态地址的重复 flag；传值设置、空值清空、edit 中省略的 key 保留原值。
