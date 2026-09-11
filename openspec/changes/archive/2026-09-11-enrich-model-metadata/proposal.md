## Why

对照各 Coding Agent 的模型配置，档案里的模型元数据不够用：Codex 缺输入模态就当纯文本，默认推理档取档位列表首项会把 MiniMax 写成 Think-Off。模型迭代快，senv 不能再为具体模型写特例。

## What Changes

- 模型元数据新增**默认推理档**（有推理档位时必填声明）与**输入模态**（可选）
- 装配优先级：显式 per-model > 档案已有 > 集合级 `--default-reasoning` > 模型目录；**不**从档位列表或模型名推断
- 无档位的模型不要求默认推理档；旧档案缺字段仍可切换，Codex 投影用模板 `none` / `["text"]`，不回写 vault
- 切换时把这两维投影进 Codex / Kimi / Pi / OpenCode；`show`、MCP、TUI 详情展示，TUI 表单可编辑

## Non-goals

- 不把 Codex plumbing（shell_type、truncation、base_instructions 等）写入档案
- 不落盘 cost / knowledge / family；不新增 models.dev 字段时不在 senv 内造默认档
- 不改 zcode/cursor（仍 unsupported）；不静默迁移旧档案

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `llm-provider`: add/edit/show 装配并展示默认推理档与输入模态
- `llm-provider-switch`: 切换按档案声明投影，缺省用 agent 模板而非推断
- `llm-provider-tui`: 表单与详情覆盖这两维

## Impact

- `internal/storage` `LLMModelInfo`、`internal/llm` 装配/目录解析/各适配器投影
- `cmd/ai_provider.go`、TUI AI Tab、MCP `model_info`、`.agents/skills/senv-cli/SKILL.md`
- 测试：装配校验、旧档案切换、Codex/Kimi/Pi/OpenCode 投影
