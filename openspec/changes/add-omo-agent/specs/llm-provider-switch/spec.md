# llm-provider-switch Delta

## MODIFIED Requirements

### Requirement: 切换 agent 指向
`senv ai switch <agent> <provider>` SHALL 校验 agent 属于支持的注册表（claude-code、codex、zcode、kimi、pi、omp、omo、opencode）且 provider 档案存在。切换 SHALL 把 provider 指向、**Agent 模型集**（默认取 Provider 模型集全集，显式给出时取保序子集）与**默认模型**写入该 agent 的原生配置，使该 agent 自己的模型选择器能在集合内切换：claude-code 写 `modelPicker`（每行 SHALL 带 `behavesAs`，映射到该版本已知模型或其 1M 形态），codex 写指向 senv 生成 catalog 文件的 `model_catalog_json`，kimi 为每个模型写一条 `[models.*]`，pi 写 `providers.<id>.models[]` 并在既有 `enabledModels` 非空时把默认模型 scope 置顶，omo 写 `providers.<id>.models[]`（条目形状与投影同 pi）到 `models.json` 并在 `settings.json` 写 `defaultProvider`/`defaultModel`、同样在既有 `enabledModels` 非空时把默认模型 scope 置顶，opencode 写 `provider.<id>.models{}`，omp 写 `models.yml` 的 `providers.<id>`（条目形状与投影同 pi）并在 `config.yml` 写 `modelRoles.default` 为 `<id>/<默认模型>`。Agent 模型集 MUST NOT 为空，每个模型 MUST 属于档案模型集；默认模型 MUST 属于 Agent 模型集。模型元数据 SHALL 优先取档案内的 per-model 信息，缺失字段才回退 models.dev 缓存；旧档案没有该信息时 MUST 保持可切换，MUST NOT 回写档案。已解析的元数据 SHALL 按各 agent 原生字段投影，避免落到 agent 内置默认：context window 写 pi/omp/omo `contextWindow`、opencode `limit.context`、kimi `max_context_size`、codex catalog `context_window`；输出上限写 pi/omp/omo `maxTokens`、opencode `limit.output`、kimi `max_output_size`；推理能力由「有 effort 档位」或显式 `reasoning: true`（无档位自适应模型）任一成立即可，此时写 pi/omp/omo/opencode `reasoning: true`、kimi `capabilities` 含 `thinking`（有档位时同时写 `support_efforts`）、codex catalog `supported_reasoning_levels`；已声明的默认推理档写 codex `default_reasoning_level`（MUST 属于档位列表）；切换 codex 时 SHALL 按本次默认模型同步顶层 `model_reasoning_effort`：默认模型有默认推理档时写该档，否则删除该键。输入模态写 codex `input_modalities`、kimi `capabilities` 的 `image_in`/`video_in`（当模态含 image/video）、pi/omp/omo `input`、opencode `modalities.input`。某模型缺失某项元数据时该字段 MUST 省略而不是写 0/false/空数组；codex catalog 的 `supported_reasoning_levels` 是例外，必须存在但允许为空数组。无推理档位或旧档案缺默认推理档时 MUST NOT 写入单档 `none`、MUST NOT 设 `default_reasoning_level`：none 是“关闭思考”的有效指令，强制思考模型会拒绝（2013）。缺输入模态时 MUST 写入 `["text"]`。切换 SHALL 清理上一次由 senv 写入、本次不再需要的条目与不再被指向的 `senv-<alias>` catalog 文件。切换前 SHALL 校验档案 `api_shape`（若声明）与目标 agent 协议族的兼容性，不兼容时 MUST 拒绝且不写任何文件；协议族内兼容时 SHALL 把声明值投影为该 agent 的线协议字段：pi/omp/omo `api` 写 `openai-responses`、kimi provider `type` 写 `openai_responses`、codex `wire_api` 恒写 `responses`（上游已移除 chat 线协议，声明不再改变它）、opencode npm 写 `@ai-sdk/openai`（声明 `openai-responses`）；未声明 `api_shape` 时各字段 SHALL 维持既有默认（pi/omp/omo `openai-completions`、kimi `openai`、codex `responses`、opencode `@ai-sdk/openai-compatible`）。档案声明 `openai-chat` 且无 `responses_base_url` 时 MUST 拒绝切换 codex 且不写任何文件（codex 只讲 Responses 线协议）；设有 `responses_base_url` 时放行，地址取该字段。校验通过后 SHALL 解密凭据引用并调用该 agent 的适配器写回配置，再更新本机指针。对已指向同一 provider 的 agent，以新默认模型重跑切换 SHALL 仅更换默认模型，Agent 模型集、provider 指向与配置中的其它字段保持不变；pi/omo 为让默认模型在非空 `enabledModels` 中优先生效而重排 senv scope 时，用户其他 scope 不受影响；omp 仅更新 `modelRoles.default`。以显式模型集重跑 SHALL 按新集合重算并清理差集。TUI SHALL 提供等价的「仅换默认模型」入口。

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
- **WHEN** 档案 `api_shape` 为 `openai-responses` 且分别切换 pi、omo、kimi、opencode
- **THEN** pi/omp/omo provider `api` 写 `openai-responses`，kimi provider `type` 写 `openai_responses`，opencode provider `npm` 写 `@ai-sdk/openai`；codex 不受此声明影响（保持 `responses`）

#### Scenario: 声明 openai-chat 时 codex 只走 responses
- **WHEN** 档案 `api_shape` 为 `openai-chat` 且用户切换 codex
- **THEN** 无 `responses_base_url` 时 MUST 拒绝且不写任何文件（codex 已移除 chat 线协议）；设有 `responses_base_url` 时 `[model_providers.<id>]` 的 `wire_api` 写 `responses` 且地址取 `responses_base_url`；未设形态地址或未声明 `api_shape` 时同样写 `responses`，地址回落推断

#### Scenario: pi 默认模型不被 enabledModels 覆盖
- **WHEN** pi 的 `settings.json` 已有非空 `enabledModels`，切换后默认模型为 m2
- **THEN** `enabledModels` 的首个 scope 解析为 `senv-<alias>/m2`，并保留 `senv-<alias>/*`；上一次 senv provider 的 scope 被清理，用户其他 scope 保持原顺序

#### Scenario: omo 默认模型不被 enabledModels 覆盖
- **WHEN** omo 的 `~/.omo/agent/settings.json` 已有非空 `enabledModels`，切换后默认模型为 m2
- **THEN** `enabledModels` 的首个 scope 为 `senv-<alias>/m2`，紧随 `senv-<alias>/*`；上一次 senv provider 的 scope 被清理，用户其他 scope 保持原顺序

#### Scenario: omo 写默认模型两键
- **WHEN** 用户切换 omo 至某 provider
- **THEN** `~/.omo/agent/settings.json` 的 `defaultProvider` 为 `senv-<alias>`、`defaultModel` 为本次默认模型；`models.json` 的其它 provider 与 `settings.json` 的其它键原样保留

#### Scenario: omo 投影上下文窗口
- **WHEN** 切换 omo 至某 provider，其档案为模型 m1 保存了 context window 300000、输出上限 32000 与推理档位
- **THEN** `~/.omo/agent/models.json` 的 `providers.<id>.models[]` 中 m1 条目含 `contextWindow: 300000`、`maxTokens: 32000` 与 `reasoning: true`

#### Scenario: codex 生成可解析 catalog
- **WHEN** 用户切换 codex 至某 provider
- **THEN** `config.toml` 的 `model_catalog_json` 指向 senv 生成的 `senv-<alias>.json`，该文件列出全部选中模型且能被 codex 解析，文件权限为 0600

#### Scenario: codex 无推理档位时不伪造 none
- **WHEN** 用户切换 codex 且某模型没有推理档位元数据
- **THEN** catalog 该条目 `supported_reasoning_levels` 为空数组，`default_reasoning_level` 省略；catalog 可被 codex 解析

#### Scenario: codex 有推理档位时写入默认档
- **WHEN** 用户切换 codex 且某模型的推理档位为 `low`、`high`，默认推理档声明为 `high`
- **THEN** catalog 该条目 `supported_reasoning_levels` 保序写出这两档，`default_reasoning_level` 为声明值 `high`，MUST NOT 取列表首项 `low`

#### Scenario: 旧档案缺默认推理档仍可切换
- **WHEN** 用户切换 codex 且某模型档案有推理档位但无默认推理档
- **THEN** catalog 该条目写该模型的档位、省略默认档；顶层 `model_reasoning_effort` 被删除；命令成功，档案不被回写；输出提示可 edit 补默认推理档

#### Scenario: codex 顶层 effort 跟随默认模型
- **WHEN** 用户切换 codex 且本次默认模型有默认推理档 `max`
- **THEN** `config.toml` 顶层 `model_reasoning_effort` 写 `max`，不再保留上一模型的 `none`

#### Scenario: 自适应模型不发送 effort
- **WHEN** 档案声明某模型 `reasoning: true` 但无 effort 档位，并把它切换为默认模型
- **THEN** catalog 该条目 `supported_reasoning_levels` 为空数组，`default_reasoning_level` 省略；顶层 `model_reasoning_effort` 被删除；其它 agent 的推理开关仍为 true

#### Scenario: 投影输入模态
- **WHEN** 档案 m1 的输入模态为 `text,image`，用户分别切换 codex、kimi、pi、omo、opencode
- **THEN** Codex catalog 写 `input_modalities: ["text","image"]`；kimi `capabilities` 含 `image_in`；pi/omp/omo 条目含 `input: ["text","image"]`；opencode 条目含 `modalities.input`

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
- **WHEN** 用户在 LLM Tab 对右栏已指向某 provider 的 agent 按 `m` 并在其 Agent 模型集内选择另一个模型
- **THEN** 指针与配置文件中的默认模型更新，Agent 模型集与 provider 指向不变，成功后刷新展示

### Requirement: 原子写回与失败回滚
适配器写回 SHALL 保留目标配置中与本功能无关的既有内容，并保持 JSON/TOML 语法有效：本功能 MUST NOT 把值插入多行 scalar、多行数组或错误表块。涉及一个 agent 的多份配置、本次新建的文件（如 codex catalog）与本次删除的条目或文件时 SHALL 作为一次事务处理：任一环节失败时，已成功的写入 SHALL 恢复到切换前内容，已删除的内容 SHALL 恢复，已新建的文件 SHALL 删除。每次写回 SHALL 使用临时文件加同目录 rename 原子替换，替换前在安全备份中保存原内容；恢复 MUST NOT 覆盖唯一好备份。同一 agent 配置路径的切换 SHALL 串行执行。切换完全成功后 SHALL 清理本功能创建的备份；切换失败且无法恢复时 SHALL 保留可用备份并在错误中说明。

#### Scenario: merge 保留既有内容
- **WHEN** 目标配置已含与本功能无关的键且执行切换
- **THEN** 写回后这些键原样保留，仅本功能负责的字段被更新

#### Scenario: 合法 TOML 不被破坏
- **WHEN** Codex TOML 已含多行数组或多行字符串等合法 TOML 值且执行切换
- **THEN** 新文件仍可被 TOML parser 解析，既有值语义不变，且不产生重复顶层键

#### Scenario: 多文件适配器回滚
- **WHEN** Pi 的第一份配置写回成功但第二份配置写回失败
- **THEN** 第一份配置恢复为切换前内容，第二份保持原状，指针不变，命令非 0 退出

#### Scenario: 写回失败不留半写
- **WHEN** 单文件适配器在序列化、重命名等写回过程出错
- **THEN** 原配置保持不变（或从备份恢复），指针不变，命令以非 0 退出

#### Scenario: 指针更新失败回滚配置
- **WHEN** 配置已写回但指针保存失败
- **THEN** 全部已写回配置恢复为切换前内容，本次新建的 catalog 文件被删除，指针不变，命令以非 0 退出

#### Scenario: 清理失败回滚本次写入
- **WHEN** 本次切换需要删除旧条目或旧 catalog 文件但删除失败
- **THEN** 本次写入的新内容一并回滚，命令以非 0 退出，指针不变

#### Scenario: 恢复失败保留好备份
- **WHEN** 指针更新失败且恢复操作也失败
- **THEN** 磁盘上保留切换前内容的有效备份，错误说明恢复失败，命令以非 0 退出

#### Scenario: 并发切换串行化
- **WHEN** CLI、TUI 或 MCP 同时对同一 agent 发起两次切换
- **THEN** 两次切换按路径串行完成，最终配置和指针只反映后完成的一次，不出现交叉写或备份互相覆盖

#### Scenario: 成功后清理备份
- **WHEN** 切换、指针更新和恢复检查全部成功
- **THEN** 本功能为本次切换创建的 `.senv-bak` 备份被删除，新配置和指针保留

### Requirement: 本机指针存储
指针 SHALL 存于 `~/.config/senv/agent-pointers.json`（权限 0600），按 agent id 记录 `(provider, models[], default_model, switched_at)`：`models` 是该 agent 配置中实际写入的 Agent 模型集，`default_model` 是其起始模型。读取时若记录只有 `model` 字段（version 1 旧格式）SHALL 视为 `models=[model]`、`default_model=model`，MUST NOT 要求用户重新切换；下一次切换写回时升级为新结构。指针是本机状态：MUST NOT 写入 vault，MUST NOT 随 vault 同步。`senv ai status` 与 switch 输出以指针为唯一事实源，不解析 agent 配置文件推断状态。

#### Scenario: 指针落盘
- **WHEN** 切换成功
- **THEN** `~/.config/senv/agent-pointers.json` 记录该 agent 的 provider、Agent 模型集、默认模型与切换时间，权限为 0600

#### Scenario: 旧指针读作单元素集
- **WHEN** 指针文件是 version 1 格式且某 agent 记录为 `model: m1`
- **THEN** 读取结果视为 `models=[m1]`、`default_model=m1`，命令不报错也不要求重新切换

#### Scenario: 指针不进 vault
- **WHEN** 切换成功后查看 vault 数据目录
- **THEN** 不存在任何指针相关条目；vault 同步不携带指针

### Requirement: 查看各 agent 当前指向
`senv ai status` SHALL 列出全部注册 agent：已切换的显示 `provider / 默认模型（N 个模型）` 与切换时间；指针记录的 Agent 模型集与档案当前模型集不一致时 SHALL 附带漂移提示，且 MUST NOT 通过解析 agent 配置文件来判定；未切换的显示未切换；cursor 显示不支持。每行 SHALL 附带该 agent 的配置文件路径。

#### Scenario: 混合状态展示
- **WHEN** claude-code 已切换、opencode 未切换、cursor 不支持且用户执行 status
- **THEN** 三行分别显示指向（含默认模型与模型条数）加时间、未切换、不支持，且各附配置路径

#### Scenario: 无指针文件
- **WHEN** 从未执行过切换且用户执行 status
- **THEN** 全部 agent 显示未切换，命令退出码为 0

#### Scenario: 漂移提示
- **WHEN** 指针记录 `provider P / models [m1, m2]`，而档案 P 的模型集已变为 `[m1]`
- **THEN** 该行附带漂移提示，说明与档案不一致，且不读取 agent 配置文件

### Requirement: 凭据按 agent 格式落地
切换时 SHALL 从 vault 解密凭据引用（`text:` 经 text manager、`env:` 经 env manager）并按 agent 原生格式写入配置：支持文件内凭据字段的 agent（claude-code、zcode、kimi、pi、omp、omo、opencode）直接写入并收敛文件权限为 0600；codex 的 TOML 配置只写 `model`/`model_provider`/`base_url`/`env_key`，凭据 MUST NOT 写入文件。codex 的切换 SHALL 同样解密凭据引用以校验其在本机存在：引用缺失时 MUST 拒绝切换且零写入（诊断含引用的完整名字与修复指引），明文 MUST NOT 落盘、MUST NOT 进入命令输出。成功输出 SHALL 含写进 `env_key` 的确切环境变量名。

#### Scenario: 文件内凭据 agent
- **WHEN** 切换 pi 至某 provider
- **THEN** `~/.pi/agent/models.json` 中对应 provider 含解密后的 apiKey，文件权限为 0600

#### Scenario: omp 文件内凭据
- **WHEN** 切换 omp 至某 provider
- **THEN** `~/.omp/agent/models.yml` 中对应 provider 含解密后的 apiKey，文件权限为 0600

#### Scenario: codex 凭据不落盘
- **WHEN** 切换 codex 至某 provider
- **THEN** `config.toml` 含模型与 provider 定义及 `env_key` 名，不含任何密钥明文

#### Scenario: codex 凭据引用缺失
- **WHEN** 档案的 `credential_ref` 指向本机 vault 中不存在的条目且切换 codex
- **THEN** 切换以非零退出、错误含该引用的完整名字与补齐指引、`config.toml` 零写入

#### Scenario: omo 默认只写环境变量引用
- **WHEN** 用户以默认 `--credential-mode=auto` 切换 omo，档案凭据引用为 `env:llm-keys/kimi`
- **THEN** `models.json` 中该 provider 的 `apiKey` 为 `"$KIMI"`，明文不落盘

#### Scenario: omo 强制 inline
- **WHEN** 用户以 `--credential-mode=inline` 切换 omo
- **THEN** `models.json` 中 `apiKey` 为解密后的明文，文件权限为 0600

### Requirement: agent id 别名查找

`mcp` 与 `llm` 两个注册表的 id 查找 SHALL 接受各目标声明的别名（omp 的别名为 `oh-my-pi`，omo 的别名为 `oh-my-openagent`），大小写不敏感；别名 MUST NOT 出现在任何列表、`ai status` 输出、报错枚举或帮助文本中——展示面只显示规范 id。

#### Scenario: 别名可查找

- **WHEN** 用户执行 `senv ai switch oh-my-pi myprovider`、`senv ai switch oh-my-openagent myprovider` 或 `senv mcp install oh-my-openagent`
- **THEN** 命令按对应规范 id（omp / omo）处理，写盘目标与规范 id 完全一致

#### Scenario: 展示面只有规范 id

- **WHEN** 用户执行 `senv ai status` 或 `senv mcp install`（无参数列表）
- **THEN** 输出中只出现规范 id，不出现任何别名

## ADDED Requirements

### Requirement: omo 配置路径解析

omo 的配置目录 SHALL 按以下顺序解析（首个非空的变量生效）：`OMO_CODING_AGENT_DIR`、`SENPI_CODING_AGENT_DIR`、`PI_CODING_AGENT_DIR`；三者全空时 MUST 用 `$HOME/.omo/agent`。路径值 MUST 原样使用。senv MUST NOT 镜像 omo 从当前目录向上查找 `.omo/agent` 的行为：`ai switch` 与 `mcp export` 的 user scope 一律写解析出的用户级目录，使同一命令在任何工作目录产生同一目标。omo 的配置路径为 `<agent dir>/models.json` 与 `<agent dir>/settings.json`；切换 SHALL 把两者纳入同一写回事务（备份、temp+rename 原子替换、失败回滚）。

#### Scenario: 默认目录
- **WHEN** 三个环境变量都未设置，用户切换 omo
- **THEN** 写回目标为 `$HOME/.omo/agent/models.json` 与 `$HOME/.omo/agent/settings.json`

#### Scenario: OMO_CODING_AGENT_DIR 优先
- **WHEN** 同时设置 `OMO_CODING_AGENT_DIR=/x` 与 `PI_CODING_AGENT_DIR=/y`，用户切换 omo
- **THEN** 写回目标为 `/x/models.json` 与 `/x/settings.json`

#### Scenario: 不跟随项目级目录
- **WHEN** 当前目录或其祖先存在 `.omo/agent` 且三个环境变量都未设置，用户切换 omo
- **THEN** 写回目标仍为 `$HOME/.omo/agent/`，不是该 `.omo/agent`

### Requirement: omo 兼容字段投影

切换 omo 写 provider 定义时 SHALL 一并写 provider 级 `compat`，其中 `supportsDeveloperRole` MUST 为 `false`。理由与 pi/omp 相同：omo 沿用 pi 的判定——`model.reasoning && compat.supportsDeveloperRole` 决定 system prompt 用 `developer` 还是 `system` 角色，缺省推断只认硬编码的 base URL/provider 特征名单，senv 写入的自建网关会被误判，使推理模型发出上游不接受的 `developer` 角色。该声明 MUST 覆盖该 provider 下全部模型，MUST NOT 出现 `supportsReasoningEffort: false`。

#### Scenario: 推理模型不发 developer 角色
- **WHEN** 用户切换 omo 至某 provider，其档案为模型 m1 保存了推理档位，接入地址为自建网关
- **THEN** `providers.<id>` 含 `compat.supportsDeveloperRole: false`，m1 条目仍含 `reasoning: true`

#### Scenario: 不连带关闭推理档位透传
- **WHEN** 切换 omo 至任一 provider
- **THEN** provider `compat` 中 MUST NOT 出现 `supportsReasoningEffort: false`
