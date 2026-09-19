# Provider 多形态接入地址（per-shape URLs）

- slug: provider-per-shape-urls
- status: archived
- created: 2026-09-19
- updated: 2026-09-19
- archived: 2026-09-19
- handed-off: 2026-09-19

## 目标

- 为一个 LLM Provider 档案支持按接口形态分别配置接入地址：`chat_base_url` / `responses_base_url` / `anthropic_base_url` 三个可选字段。
- 某形态地址未设置时自动推断：沿用 ADR-0004 规则从必填的 `BaseURL`（语义收窄为默认地址 + 推断源）按协议族推导。
- 完整决策与规则明细见 [design/adr-per-shape-urls.md](design/adr-per-shape-urls.md)。

## 非目标

- 不做 per-agent 地址覆盖（agent 矩阵不进 vault schema）。
- 不从 URL 内容推断形态（ADR-0006 明确拒绝的路径）。
- 不允许省略 `BaseURL`（即使三个形态地址全配）。
- 存量档案不迁移（字段缺省即旧行为）。

## 现状

- `LLMProviderEntry` 只有单一 `BaseURL` 字段（`internal/storage/types.go:139`），统一按 OpenAI 兼容形态落库（补末段 `/v1`、收敛尾斜杠；纯数字版本段如 `/v4` 视为已归一，commit b69a657）。
- `api_shape`（`openai-chat | openai-responses | anthropic`，ADR-0006）只声明形态，作为 switch 兼容判据（`internal/llm/switch.go:981`）并在 OpenAI 族内选线协议，不携带独立地址。
- ADR-0004：一份档案一个接入地址，switch 按目标 agent 协议族转换（claude-code 剥离末段 `/v1`，对 Anthropic 族是无损变换）；「per-agent 覆盖接入地址」两次被推迟为逃生舱，「等真出现第二个不兼容端点再加」——本任务即该触发条件。
- 现有变通：开两个 provider 别名指向两个地址，凭据经 `--key-ref` 共享，各自声明 `--api-shape`。
- 产品约定（AGENTS.md）：CLI 与 TUI 是两个一等交互面，用户可见能力默认两边同步评估。
- MCP `llm_provider_list` 视图是显式白名单（`cmd/mcp_llm.go:14`），含 `base_url`（地址按非密钥对待），但当前缺 `api_shape` 字段。

## 进展

- 2026-09-19：确认当前 schema 仅单一 BaseURL，无 per-shape/per-agent 地址机制；立项探索。
- 2026-09-19：两轮 grilling 后 frontier 清空；schema、门禁、地址选择、归一化、三面暴露与非目标全部敲定，术语落 glossary.md，决策落 design/adr-per-shape-urls.md。

## 决策

- 采纳：per-shape 形态地址方案 — `BaseURL` 保留必填 + 三个可选形态字段，未设自动推断；详见 [design/adr-per-shape-urls.md](design/adr-per-shape-urls.md)。
- 取舍：接受「线协议决定地址、地址存在性只参与门禁」的严格规则（放弃地址存在性牵引线协议的直觉规则）；接受 anthropic 字段原样存储、不做 `/v1` 归一。
- 带进实现的未决：无（frontier 已清空）。
- 回退：新字段均可选、存量零迁移；回退即停止使用字段，无需 schema 迁移。

## 交接

- driver: `provider-per-shape-urls-driver`
- 采纳方案：per-shape 形态地址（`BaseURL` 必填保留 + `chat_base_url` / `responses_base_url` / `anthropic_base_url` 三个可选字段，未设自动推断；门禁放宽与地址选择规则见 ADR）
- design 指针：[design/adr-per-shape-urls.md](design/adr-per-shape-urls.md)、[glossary.md](glossary.md)
- 可带进实现的未决：无（frontier 已清空）
- 探索任务归档路径：`tasks/archive/2026-09-19/provider-per-shape-urls/`

## 未决问题

- 无：explore 结束时 frontier 已清空；实现期细节（switch/baseurl 拆分、schema 校验、三面同步、SKILL.md 与仓库 ADR 晋升）见 [design/adr-per-shape-urls.md](design/adr-per-shape-urls.md) 的 Consequences。

## 下一步

- decide：冻结方案后 handoff 给 taskflow（`provider-per-shape-urls-driver`）；如需实现级方案稿可先 design。
