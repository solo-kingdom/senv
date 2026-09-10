# Grill：agent-model-set

## 决策记录

| # | 决策 | 结论 | 理由 | 状态 |
|---|------|------|------|------|
| D1 | 「在 agent 内切换」指哪一层 | agent 原生模型选择器；senv 只写 Agent 模型集与默认模型 | 换模型不该离开 agent；senv 不做第二套模型选择器 | settled |
| D2 | 两个「默认模型」的关系 | 默认取 provider 档案的默认模型；`--default-model` 只覆盖本次且不回写；档案无默认则报错 | 档案是默认值的单一来源；静默取第一项是隐式选择 | settled |
| D3 | 「默认全选」的边界 | 全选 Provider 模型集、不静默截断；输出条数，>20 时提示可用 `--models` 缩小 | 用户明确要求默认全选；截断会让档案里的模型凭空消失 | settled |
| D4 | 选中列表的状态与漂移 | 当前指向扩展为 provider + Agent 模型集 + 默认模型；档案变更不自动回写，切换时重算并提示漂移 | 指针是本机事实源、status 不读 agent 配置；记列表才谈得上漂移 | settled |
| D5 | claude-code 投影 | `settings.json` 的 `modelPicker.options` + `replaceBuiltInOptions: true` | provider 是自定义接入地址，内置 lineup 打过来必失败，不该出现在选择器里 | settled |
| D6 | codex 投影 | 生成 `~/.codex/model-catalogs/senv-<alias>.json`，`model_catalog_json` 指向它 | 本机已在用该机制；`[profiles]` 只能启动时切，不是会话内选择器；降级为单模型损失太大 | settled |
| D7 | 模型元数据来源 | kimi `max_context_size` 取 models.dev `limit.context`，缺失回退 131072；pi/opencode 只写 id/name | 目录里有真实值就不猜；其余 agent 不强制元数据 | settled |
| D8 | CLI 参数形状 | 新增 `--models`/`--default-model`，移除 `--model`（出现即报错并提示新用法） | 旧语义是「指向某个模型」，新语义是「选定集 + 默认」，静默兼容会让语义含糊 | settled |
| D9 | 「仅换默认模型」语义 | 只改默认模型、必须在现有 Agent 模型集内，不动列表；换列表只能重新 switch | 与 D4 一致：列表只在显式切换时重算 | settled |
| D10 | 残留清理与字段所有权 | 按「当前指向 + 本次 Agent 模型集」清差集，删失效 `senv-*.json`；用户自有键不动，仅留 `.senv-bak` | 不清理则 A→B 后选择器混挂两家；清理用户自有键则越界 | settled |
| D11 | 指针迁移 | 版本仍为 1、新字段可选；旧指针读作单元素集 | 旧指针描述的正是「配置里只有这一个模型」，是事实而非猜测 | settled |

## 术语表

| 术语 | 本任务语境下的定义 | 与既有用词的关系 |
|------|--------------------|------------------|
| Provider 模型集 | 一份 LLM Provider 档案声明的全部可用模型 | 新造；原先只隐含在「可用模型集」措辞里，已入 `CONTEXT.md` |
| Agent 模型集 | 某次切换写入某个 agent、供其在自己的选择器里切换的模型子集（默认全选） | 新造；修正了「切换」原先的单模型语义，已入 `CONTEXT.md` |
| 默认模型 | 切换后 agent 起始使用的单个模型，默认取档案默认模型、可单次覆盖不回写 | 新造；已入 `CONTEXT.md` |
| 漂移（Drift） | senv 侧事实源与 agent 侧派生产物不再一致、且 senv 不回读 agent 配置去纠正的状态 | 沿用 ADR-0007 的 MCP 导出漂移语义并推广到模型集，已入 `CONTEXT.md` |

## ADR 候选

<!-- 仅记录满足「难逆转 + 后人费解 + 真实取舍」三门槛的；由 design.md 吸收或随 change 归档晋升 -->

- [x] adr-agent-model-set-projection: 切换写模型集而非单模型，并按各 agent 原生机制投影；codex 需合成 catalog 元数据（出处：D1/D5/D6/D7）→ 已记入 `docs/adr/0011-agent-model-set-projection.md`（proposed）
- [x] adr-senv-owns-isolated-agent-config-namespace: 只改 `senv-<alias>` 命名空间与本次必须改的键，并在切换时清理自己上一次的痕迹（出处：D10/D11）→ 已记入 `docs/adr/0012-senv-owns-isolated-agent-config-namespace.md`（proposed）

## 未决问题

无
