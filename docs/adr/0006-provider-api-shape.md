# 0006-provider-api-shape

LLM Provider 新增可选字段 `api_shape`（`openai-chat` | `openai-responses` | `anthropic`），声明这份档案服务的接口形态。留空时沿用 ADR-0004 的行为：不猜，写配置时按目标 Coding Agent 的协议族归一接入地址。非空时以 provider 为准，并用于判定 switch 目标是否兼容——形态与 agent 协议族不匹配时拒绝写入，而不是把配置写坏。本次只覆盖接入地址归一与兼容判定，不改变各 agent 自行拼接 `/chat/completions` 或 `/responses` 的行为。

## Considered Options

- **由 senv 从 base URL 推断形态**（如含 `anthropic` 即判 Anthropic 族）：少一次输入，但把猜测放进安全关键路径；ADR-0004 已因「归一化是有损猜测」付过一次代价，不再加第二次。
- **per-agent 覆盖接入地址**（ADR-0004 记录的逃生舱）：语义更精确，但把 agent 矩阵带进 vault schema；provider 级形态已能覆盖「同一份档案服务多个 agent」的场景。

## Consequences

- 部分取代 ADR-0004：一份档案一个接入地址、跨族由 senv 归一仍然成立；「形态只由 agent 协议族决定」不再成立。
- 存量档案不迁移：字段缺省即旧行为，`api_shape` 只在用户显式设置后生效。
- 兼容判定给 `senv ai switch` 增加一种失败模式（形态不匹配），错误信息必须给出两个可行动作：改 provider 形态，或换 provider。

## Status

采纳，后经 [ADR-0027](./0027-provider-per-shape-urls.md) 修订：显式形态地址（per-shape URLs）的存在即「该族被服务」的声明，`switch` 门禁在目标族有显式地址时放行；`api_shape` 的职责收敛为 OpenAI 族内选线协议与无显式地址时的兼容判据。codex 的线协议经 [ADR-0028](./0028-codex-responses-only-wire.md) 钉死为 responses，不再是选线协议的消费方。
