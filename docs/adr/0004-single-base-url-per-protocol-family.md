# 0004-single-base-url-per-protocol-family

LLM Provider 档案只存一份接入地址，按 OpenAI 兼容形态落库（补末段 `/v1`、收敛尾斜杠）；`senv ai switch` 写配置时按目标 agent 的协议族转换，Anthropic 族（claude-code）剥离末段 `/v1`。理由是两族的接入点形态不可互换——实测把带 `/v1` 的地址写进 claude-code 会请求 `/v1/v1/messages` 得到 404，而每 agent 存一份地址会把 agent 矩阵写进 vault schema，让同一份 provider 在多机多工具间失去可比性，且新增 agent 就变成一次档案迁移。

## Considered Options

- **每 agent 存一份接入地址**（档案内 per-agent 覆盖）：语义精确，但把 agent 矩阵带进 vault schema；已作为逃生舱推迟，等真出现第二个不兼容端点再加。
- **统一补 `/v1`、不做转换**：对 claude-code 是回归（`/v1/v1/messages`），且与它当前可用的配置相矛盾。
- **存 root、各 agent 自行拼接**：无法还原 `https://host/api/llm/v1` 这类带路径前缀的真实端点。

## Consequences

- 剥离末段 `/v1` 对 Anthropic 族是**无损**变换：Claude Code 总请求 `base + /v1/messages`，剥离前后的最终 URL 完全相同。改 `trimTrailingV1` 前需理解这一点。
- 归一化是有损猜测：`/v1beta` 这类版本变体会被补成 `…/v1beta/v1`。补偿是 `ai switch` 输出实际写入的接入地址，让改写可见；档案不加额外字段。（2026-09-19 细化：末段为纯数字版本段（`/v4` 这类，智谱 open.bigmodel.cn 实测 `/v4/v1` 404）视为已归一不再补 `/v1`；带字母后缀的变体维持有损语义。）
- 存量档案不迁移：adapter 读取侧再做一次幂等归一，旧档案在下次 `switch` 时自动修正；已被写坏的 agent 配置需重跑 `switch`，不自动改写。

## Status

部分由 [ADR-0006](./0006-provider-api-shape.md) 取代：接入地址的形态可由 LLM Provider 显式声明（`api_shape`），不再只由 agent 协议族决定；归一规则本身不变。
