# ADR 草稿：Provider 形态地址（per-shape URLs）

> 任务内草稿（task-explore 产出）。实现交付时晋升到仓库 `docs/adr/`，并标注对 ADR-0004/0006 的修订关系。

## 决策

LLM Provider 档案在保留单一 `BaseURL`（必填，语义收窄为默认地址 + 推断源）的基础上，新增三个**可选**形态地址字段：

- `chat_base_url`（openai-chat 端点 root）
- `responses_base_url`（openai-responses 端点 root）
- `anthropic_base_url`（Anthropic Messages 端点 root，即 claude-code 拼 `+ /v1/messages` 的 base）

某形态地址未设置时，沿用 ADR-0004 规则从 `BaseURL` 推断（「未设置则自动推断」）。存量档案零迁移：字段缺省即旧行为。

## 背景

ADR-0004 确立「一份档案一个接入地址」，并把 per-agent/per-形态覆盖推迟为逃生舱（「等真出现第二个不兼容端点再加」）。真实网关中三个端点不共根（chat/responses 也可能不同根），单地址 + 推断覆盖不了该场景——本决策即该逃生舱的兑现，粒度开到 **per-shape**（per-agent 仍不做）。

## Considered Options

- **三个平铺可选字段（采纳）**：shapes 是封闭三元枚举，平铺字段让 CLI flag、TUI 表单、校验、MCP 视图都最直接。
- `shape_urls` map：扩展性冗余（枚举封闭），各消费面变绕。放弃。
- 去掉 `BaseURL`、三字段全显式：破坏存量档案与「一份档案多机多工具可比性」。放弃。
- per-agent 地址覆盖：agent 矩阵继续不进 vault schema（ADR-0004 结论维持）。放弃。

## 规则明细

### 门禁（switch 兼容判定）

设目标 agent 协议族为 F（claude-code → anthropic；codex/kimi/pi/opencode → openai）：

1. F 存在显式形态地址（F=openai 时 `chat_base_url` 或 `responses_base_url` 任一存在；F=anthropic 时 `anthropic_base_url` 存在）→ **放行**；
2. `api_shape` 未声明 → 放行（旧行为）；
3. `api_shape` 的协议族 == F → 放行（旧行为）；
4. 否则拒绝，不写任何文件；错误文案给三个可行动作：**改档案形态 / 补配该族地址（`--shape-url <shape>=<url>`）/ 换 provider**。

### 地址选择

- Anthropic 族（claude-code）：用 `anthropic_base_url`（若设），否则从 `BaseURL` 推断（剥末段 `/v1`）。
- OpenAI 族：线协议 W 由 `api_shape`（openai-* 声明）或 agent 既有默认决定；地址 = W 对应的形态字段（若设），否则回落 `BaseURL`。**形态地址的存在性只参与门禁，从不牵引 W**（严格规则；不从地址存在性做推断）。

### 归一化与校验

- OpenAI 族形态字段：沿用现规则（补末段版本段、收敛尾斜杠；纯数字版本段如 `/v4` 视为已归一）。
- `anthropic_base_url`：**原样存储**，仅收敛尾斜杠；不做任何 `/v1` 魔法（`/api/anthropic` 这类前缀不可被版本段规则触碰）。
- 三字段与 `BaseURL` 同受 https 校验与 `--allow-http` 门禁；URL 合法性校验同 `ValidateLLMProviderURL`。

### 各交互面

- **CLI**：重复 flag `--shape-url <api_shape>=<url>`（house style 同 `--model-context`）；`add` 即设，`edit` 传值设置、空值（`--shape-url anthropic=`）清空、省略的 key 保留原值。
- **TUI**：provider 表单渲染三个固定字段；详情弹层补形态地址行；枚举封闭不需要动态控件。
- **MCP `llm_provider_list`**：白名单视图新增 `shape_urls`（`{chat?, responses?, anthropic?}`，omitempty）与 `api_shape`（当前视图缺失，顺手补齐）。地址按既有 `base_url` 先例视为非密钥。
- **`switch` 输出**：按目标 agent 输出最终写入地址 + 来源（`explicit: <字段名>` / `inferred: BaseURL`），延续 ADR-0004「有损变换用输出补偿可见性」原则。

## 非目标

- per-agent 地址覆盖（agent 矩阵不进 schema）。
- 从 URL 内容推断形态（ADR-0006 明确拒绝）。
- 允许省略 `BaseURL`（即使三形态地址全配）。
- 存量档案迁移。

## Consequences

- `internal/llm/switch.go` 门禁新增放行分支，错误文案更新；`internal/llm/baseurl.go` 归一化需按族拆分（openai 族规则 vs anthropic 字段透传）。
- `internal/storage/types.go` schema 扩三个字段 + 校验；加密 blob 内变更，sync 通道与 server 白名单无需改动。
- 实现交付时：晋升本稿为仓库 ADR（标注对 0004/0006 的修订：单地址原则收窄为「单默认地址 + 可选形态地址」；api_shape 职责收敛）；回写 `.agents/skills/senv-cli/SKILL.md`；CLI/TUI/MCP 三面同步（AGENTS.md 一等交互面约定）。

## 决策记录（grilling 轮次）

- R1（Q1–Q7）：三字段设计（chat/responses 不假定同根）；schema 选平铺字段 + BaseURL 保留；per-shape 地址放行族门禁；OpenAI 族内地址跟随线协议；anthropic 字段原样存储；`--shape-url` 重复 flag；非目标四项冻结；switch 输出地址来源。
- R2（Q8–Q10）：严格规则（地址不牵引线协议）确认；MCP 视图扩容 + 补 `api_shape` 确认；拒绝文案三动作确认。
