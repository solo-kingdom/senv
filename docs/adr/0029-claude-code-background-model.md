# 0029-claude-code-background-model

LLM Provider 档案新增显式声明字段 `background_model`（必须属于该档案模型集），`senv ai switch claude-code` 按「`--background-model` 单次覆盖（不回写档案）> 档案声明值 > 默认模型」解析后台模型，写入 `~/.claude/settings.json` 的 `env.ANTHROPIC_SMALL_FAST_MODEL` 与同值的 `env.ANTHROPIC_DEFAULT_HAIKU_MODEL`（后者把 haiku 档位整体重映射：网关上无 haiku 时，标题生成等 haiku 档位调用同样静默失败）；这两个键归 senv 拥有（边界同 ADR-0012），切换即覆盖用户手写值。动机是 2026-09 msa 事故：claude-code 自动压缩用后台小模型（缺省 `claude-haiku-4-5`），自建网关（new-api）无此模型，压缩永远 503 → context 无限膨胀、每轮全量重发（实测单会话 755k token）。根因是「后台模型缺省值在这类网关上必然不存在」，因此缺省回退默认模型保证任何档案切换后压缩可用，而不是不写该键。交互流程（TUI 向导、CLI 交互）里后台模型是 claude-code 的独立步骤，带说明文案、值可留空；非交互调用缺省回退默认模型并给 warning。目前仅 claude-code 消费该字段，其他 agent 出现同类机制时再投影。`CLAUDE_CODE_AUTO_COMPACT_WINDOW` 等其余 env 键明确不归 senv 管理：压缩阈值的 per-model 投影通道是 modelPicker 的 `behavesAs`（随 picker 逐模型生效），全局静态 env 与 picker 切模型会脱节。

## Considered Options

- **按名字自动推断后台模型**（含 flash/haiku/mini 即选）：违反「显式声明，senv 不按模型名推断」的既定原则（见 CONTEXT.md 模型元数据），且推断错时静默写出网关不存在的模型——正是本事故的形态。
- **未声明时不写该键（只 warning）**：修复目标落空；缺省 haiku 在自建网关上必然不存在，压缩继续坏。
- **仅切换参数、不入档案**：每台机器每次切换都要记得指定，而「要靠人记得手工修」正是事故土壤；档案声明一次、随 vault 同步、所有机器切换自动带上。
- **固定写死默认模型、不开放声明**：压缩一定可用，但剥夺了用户选便宜模型的权利；留作缺省回退而非唯一行为。

## Consequences

- 档案 schema 新增可选字段，旧档案无此字段时回退默认模型，无需迁移；`ai provider add/edit`、TUI 编辑表单（ADR-0005）、MCP 工具同步获得该字段入口。
- 回退路径用主模型做压缩，单次压缩更贵，但远比压缩失败后的全量重发便宜。
- 双重 fail-closed 校验：`add/edit` 写档案时与切换时都要求后台模型属于 Provider 模型集（档案可能被旧版本写入）。
- 用户手写在 settings.json 的 `ANTHROPIC_SMALL_FAST_MODEL` / `ANTHROPIC_DEFAULT_HAIKU_MODEL` 会被切换覆盖：`--help` 与 skill 文档须声明这两个键由 senv 管理。
- `behavesAs` 的 `[1m]` 阈值虚高问题不在本 ADR：投影如实反映档案元数据，错的是声明值，用 `senv ai provider edit` 修数据。

## Status

accepted
