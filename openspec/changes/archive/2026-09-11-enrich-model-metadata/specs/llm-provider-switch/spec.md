## MODIFIED Requirements

### Requirement: 切换 agent 指向
`senv ai switch <agent> <provider>` SHALL 校验 agent 属于支持的注册表（claude-code、codex、zcode、kimi、pi、opencode）且 provider 档案存在。切换 SHALL 把 provider 指向、**Agent 模型集**（默认取 Provider 模型集全集，显式给出时取保序子集）与**默认模型**写入该 agent 的原生配置，使该 agent 自己的模型选择器能在集合内切换：claude-code 写 `modelPicker`（每行 SHALL 带 `behavesAs`，映射到该版本已知模型或其 1M 形态），codex 写指向 senv 生成 catalog 文件的 `model_catalog_json`，kimi 为每个模型写一条 `[models.*]`，pi 写 `providers.<id>.models[]` 并在既有 `enabledModels` 非空时把默认模型 scope 置顶，opencode 写 `provider.<id>.models{}`。Agent 模型集 MUST NOT 为空，每个模型 MUST 属于档案模型集；默认模型 MUST 属于 Agent 模型集。模型元数据 SHALL 优先取档案内的 per-model 信息，缺失字段才回退 models.dev 缓存；旧档案没有该信息时 MUST 保持可切换，MUST NOT 回写档案。已解析的元数据 SHALL 按各 agent 原生字段投影，避免落到 agent 内置默认：context window 写 pi `contextWindow`、opencode `limit.context`、kimi `max_context_size`、codex catalog `context_window`；输出上限写 pi `maxTokens`、opencode `limit.output`、kimi `max_output_size`；推理档位非空时写 pi/opencode `reasoning: true`、kimi `capabilities` 含 `thinking` 与 `support_efforts`、codex catalog `supported_reasoning_levels`；已声明的默认推理档写 codex `default_reasoning_level`（MUST 属于档位列表）；输入模态写 codex `input_modalities`、kimi `capabilities` 的 `image_in`/`video_in`（当模态含 image/video）、pi `input`、opencode `modalities.input`。某模型缺失某项元数据时该字段 MUST 省略而不是写 0/false/空数组，但 codex catalog 的推理档位与输入模态除外：无档位或旧档案缺默认推理档时 MUST 写入单档 `none` 并设 `default_reasoning_level` 为 `none`（MUST NOT 取档位列表首项）；缺输入模态时 MUST 写入 `["text"]`。这两处是 agent 投影模板，不是 senv 对模型事实的推断。空的 `supported_reasoning_levels` 会使 Codex 会话内 `/model` 选择器无法关闭。切换 SHALL 清理上一次由 senv 写入、本次不再需要的条目与不再被指向的 `senv-<alias>` catalog 文件。切换前 SHALL 校验档案 `api_shape`（若声明）与目标 agent 协议族的兼容性，不兼容时 MUST 拒绝且不写任何文件；协议族内兼容时 SHALL 把声明值投影为该 agent 的线协议字段：pi `api` 写 `openai-responses`、kimi provider `type` 写 `openai_responses`、codex `wire_api` 写 `chat`（声明 `openai-chat`）、opencode npm 写 `@ai-sdk/openai`（声明 `openai-responses`）；未声明 `api_shape` 时各字段 SHALL 维持既有默认（pi `openai-completions`、kimi `openai`、codex `responses`、opencode `@ai-sdk/openai-compatible`）。校验通过后 SHALL 解密凭据引用并调用该 agent 的适配器写回配置，再更新本机指针。对已指向同一 provider 的 agent，以新默认模型重跑切换 SHALL 仅更换默认模型，Agent 模型集、provider 指向与配置中的其它字段保持不变；pi 为让默认模型在非空 `enabledModels` 中优先生效而重排 senv scope 时，用户其他 scope 不受影响。以显式模型集重跑 SHALL 按新集合重算并清理差集。TUI SHALL 提供等价的「仅换默认模型」入口。

#### Scenario: 切换成功
- **WHEN** 用户执行 `senv ai switch claude-code myprovider`，档案模型集为 m1、m2
- **THEN** agent 配置写出该 provider 的 base_url/凭据与 `modelPicker`（含 m1、m2，替换内置 lineup）、`model` 为档案默认模型，指针记录 provider 与 `[m1, m2]`

#### Scenario: claude-code 自定义模型有已知行为映射
- **WHEN** claude-code 的 `modelPicker` 写入自定义模型
- **THEN** 每个 option 含 `behavesAs` 映射到 Claude Code 已知模型；目录声明的上下文达到 1M 时映射到该模型的 1M 形态，实发模型 ID 不变

#### Scenario: 使用档案内模型元数据
- **WHEN** provider 档案保存了 1M context window，但本机没有 models.dev 缓存
- **THEN** Claude Code 行仍写 `behavesAs` 的 1M 形态，不按未知模型的 200K 回退

#### Scenario: pi 投影上下文窗口
- **WHEN** 切换 pi 至某 provider，其档案为模型 m1 保存了 context window 300000、输出上限 32000 与推理档位
- **THEN** `providers.<id>.models[]` 中 m1 条目含 `contextWindow: 300000`、`maxTokens: 32000` 与 `reasoning: true`，不再落到 pi 内置默认 128000/16384

#### Scenario: opencode 投影 limit 与推理
- **WHEN** 切换 opencode 至某 provider，其档案为模型 m1 保存了 context window 与输出上限
- **THEN** `provider.<id>.models{}` 中 m1 条目含 `limit.context`/`limit.output` 与 `reasoning: true`；无元数据的模型 MUST NOT 出现 `limit` 键

#### Scenario: kimi 投影输出上限与 thinking
- **WHEN** 切换 kimi 至某 provider，其档案为模型 m1 保存了输出上限与推理档位
- **THEN** `[models."senv-<alias>/m1"]` 含 `max_output_size`、`capabilities` 含 `thinking` 与 `support_efforts`

#### Scenario: 声明 openai-responses 选择线协议
- **WHEN** 档案 `api_shape` 为 `openai-responses` 且分别切换 pi、kimi、opencode
- **THEN** pi provider `api` 写 `openai-responses`，kimi provider `type` 写 `openai_responses`，opencode provider `npm` 写 `@ai-sdk/openai`；codex 不受此声明影响（保持 `responses`）

#### Scenario: 声明 openai-chat 时 codex 写 chat
- **WHEN** 档案 `api_shape` 为 `openai-chat` 且用户切换 codex
- **THEN** `[model_providers.<id>]` 的 `wire_api` 写 `chat`，`api_shape` 未声明时保持 `responses`

#### Scenario: pi 默认模型不被 enabledModels 覆盖
- **WHEN** pi 的 `settings.json` 已有非空 `enabledModels`，切换后默认模型为 m2
- **THEN** `enabledModels` 的首个 scope 解析为 `senv-<alias>/m2`，并保留 `senv-<alias>/*`；上一次 senv provider 的 scope 被清理，用户其他 scope 保持原顺序

#### Scenario: codex 生成可解析 catalog
- **WHEN** 用户切换 codex 至某 provider
- **THEN** `config.toml` 的 `model_catalog_json` 指向 senv 生成的 `senv-<alias>.json`，该文件列出全部选中模型且能被 codex 解析，文件权限为 0600

#### Scenario: codex 无推理档位时写入 none
- **WHEN** 用户切换 codex 且某模型没有推理档位元数据
- **THEN** catalog 该条目 `supported_reasoning_levels` 含且仅含 `effort=none`，`default_reasoning_level` 为 `none`

#### Scenario: codex 有推理档位时写入默认档
- **WHEN** 用户切换 codex 且某模型的推理档位为 `low`、`high`，默认推理档声明为 `high`
- **THEN** catalog 该条目 `supported_reasoning_levels` 保序写出这两档，`default_reasoning_level` 为声明值 `high`，MUST NOT 取列表首项 `low`

#### Scenario: 旧档案缺默认推理档仍可切换
- **WHEN** 用户切换 codex 且某模型档案有推理档位但无默认推理档
- **THEN** catalog 该条目按无档位模板写入单档 `none`，命令成功，档案不被回写；输出提示可 edit 补默认推理档

#### Scenario: 投影输入模态
- **WHEN** 档案 m1 的输入模态为 `text,image`，用户分别切换 codex、kimi、pi、opencode
- **THEN** Codex catalog 写 `input_modalities: ["text","image"]`；kimi `capabilities` 含 `image_in`；pi 条目含 `input: ["text","image"]`；opencode 条目含 `modalities.input`

#### Scenario: 缺输入模态时 Codex 写 text 模板
- **WHEN** 用户切换 codex 且某模型档案没有输入模态
- **THEN** catalog 该条目 `input_modalities` 为 `["text"]`，档案不被回写

#### Scenario: 显式模型集保序
- **WHEN** 用户以显式模型集 `m2,m1` 切换
- **THEN** 配置按 m2、m1 的顺序写入这两个模型，指针记录同一顺序

#### Scenario: 模型集为空
- **WHEN** 用户显式给出的模型集为空
- **THEN** 命令以非 0 退出，不写任何文件

#### Scenario: 模型不属于档案
- **WHEN** 显式模型集包含不属于 Provider 模型集的模型
- **THEN** 命令以非 0 退出并列出可用模型，不写任何文件

#### Scenario: 模型缺省且无法推断
- **WHEN** 档案没有默认模型且用户未显式指定默认模型
- **THEN** 命令以非 0 退出并要求显式指定，不静默取模型集首项，不写任何文件

#### Scenario: 默认模型不属于 Agent 模型集
- **WHEN** 默认模型不在本次 Agent 模型集内
- **THEN** 命令以非 0 退出，不写任何文件

#### Scenario: agent 不在注册表
- **WHEN** `<agent>` 为 cursor 或其他未注册 id
- **THEN** 命令以非 0 退出并说明该 agent 不受支持，不写任何文件

#### Scenario: provider 档案不存在
- **WHEN** `<provider>` 在 vault 中无档案
- **THEN** 命令以非 0 退出并提示先执行 `senv ai provider add`，不写任何文件

#### Scenario: 接入形态不兼容
- **WHEN** 档案 `api_shape` 为 `openai-chat` 且用户执行 `senv ai switch claude-code <provider>`
- **THEN** 命令以非 0 退出，说明形态与 agent 协议族不兼容并给出「改档案形态或换 provider」两个动作，不写任何文件

#### Scenario: 仅更换模型
- **WHEN** claude-code 当前指向 `myprovider` 且 Agent 模型集为 m1、m2，用户以默认模型 m2 重跑切换
- **THEN** 只有默认模型变为 m2，Agent 模型集、provider、接入地址与凭据引用保持不变

#### Scenario: 缩小模型集清理旧条目
- **WHEN** 上一次写入的 Agent 模型集为 m1、m2，本次以 `m1` 重新切换
- **THEN** 配置中只剩 m1 的 senv 条目，m2 对应的 senv 条目被删除

#### Scenario: 换 provider 清理旧痕迹
- **WHEN** 同一 agent 从 provider A 切到 provider B
- **THEN** A 的 senv 条目与不再被指向的 `senv-*.json` catalog 文件被删除，B 的条目与 catalog 写入

#### Scenario: 用户自有配置不被清理
- **WHEN** agent 配置中存在用户自己写的 provider/模型条目或自有 catalog 文件
- **THEN** 它们原样保留：senv 只改自己命名空间与本次必须改的键，不删除自有 catalog 文件

#### Scenario: TUI 仅换模型
- **WHEN** 用户在 AI Tab 对右栏已指向某 provider 的 agent 按 `m` 并在其 Agent 模型集内选择另一个模型
- **THEN** 指针与配置文件中的默认模型更新，Agent 模型集与 provider 指向不变，成功后刷新展示
