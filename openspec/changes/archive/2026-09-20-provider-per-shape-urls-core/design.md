# Design

## Context

`LLMProviderEntry`（`internal/storage/types.go:137`）只有 `BaseURL` 一个地址字段，落库按 OpenAI 兼容形态归一（`internal/llm/baseurl.go`：补末段版本段、收敛尾斜杠、纯数字版本段视为已归一）。`api_shape`（ADR-0006）只声明形态并作为 switch 兼容判据（`internal/llm/switch.go` 形态校验处）。方案已由探索任务冻结（driver proposal 引用的任务 ADR），本设计只做实现映射。

## Goals / Non-Goals

**Goals:**

- 三个可选形态地址字段的存储、校验与归一化
- switch 门禁放宽、地址解析与来源可见
- CLI `--shape-url` 与 `show`/`list` 展示

**Non-Goals:**

- per-agent 覆盖、URL 内容推断、省略 `BaseURL`、存量迁移（见 driver Non-goals）
- TUI / MCP / SKILL.md / ADR 晋升（surfaces 切片）

## Decisions

- **schema 平铺字段而非 map**：shapes 是封闭三元枚举，平铺字段让 JSON、flag、校验、展示都直接。Go 字段 `ChatBaseURL` / `ResponsesBaseURL` / `AnthropicBaseURL`，JSON `chat_base_url` / `responses_base_url` / `anthropic_base_url`，均 omitempty，空串 = 未声明。
- **归一化分族落点**：`internal/llm/baseurl.go` 现有 OpenAI 族归一保持不动，OpenAI 族形态字段复用之；新增 Anthropic 形态归一 = 收敛尾斜杠 + 既有 URL 合法性校验，不做任何版本段处理。storage 写入路径按 `--shape-url` 的 key 选择归一函数。
- **门禁算法**（替换 `switch.go` 现有形态校验分支）：
  1. 目标族存在显式形态地址（F=anthropic：`AnthropicBaseURL != ""`；F=openai：`ChatBaseURL != "" || ResponsesBaseURL != ""`）→ 放行；
  2. 否则 `api_shape` 未声明 → 放行（旧行为）；
  3. 否则声明族 == 目标族 → 放行（旧行为）；
  4. 否则拒绝且零写入，错误给三个动作：改档案形态 / 补 `--shape-url <shape>=<url>` / 换 provider。
- **地址解析**（写回前）：
  - claude-code：`AnthropicBaseURL` 非空则**原样**写回（不剥版本段）；否则走现推断路径（`BaseURL` 剥末段 `/v1`）。
  - OpenAI 族：W = 声明的 openai-* 形态或 agent 既有默认（`api_shape=anthropic` 或未声明时不约束 OpenAI 族内选择）；W=chat → `ChatBaseURL`，W=responses → `ResponsesBaseURL`；字段空回落 `BaseURL`（带版本段形态原样或按需归一，沿用现规则）。
  - 输出：成功输出含实际写入地址与来源（显式字段名 / 由 BaseURL 推断）；具体文案实现定，两个信息必须齐备。
- **CLI flag**：`--shape-url` 为可重复 `key=value` pflag，key 必须 ∈ APIShapes（非法 key 报参数错误）；`add` 出现即设置；`edit` 传值设置、`--shape-url <key>=`（空值）清空、未出现的 key 保留原值。空值 key 仍须合法。

## Risks / Trade-offs

- `anthropic_base_url` 透传可能存下带 `/v1` 的值，claude-code 拼出 `/v1/v1/messages`：接受——不做隐式改写（`/api/anthropic` 前缀不可被版本段规则触碰），switch 输出实际写入值让用户可见（ADR-0004 补偿原则），SKILL.md 写明约定。
- 「地址存在性不牵引协议选择」的严格规则反直觉（如只配 responses 地址时未声明档案的 codex 走自身默认）：用 show/TUI 展示与 switch 输出来源缓解；探索任务已明确取舍。
- 与 `BaseURL` 并存的心智负担：`BaseURL` 收窄为默认地址 + 推断源，语义单一。

## Open Questions

- 无（探索任务 frontier 已清空）
