# llm-provider-switch Delta

## MODIFIED Requirements

### Requirement: 切换 agent 指向
`senv ai switch <agent> <provider>` SHALL 校验 agent 属于支持的注册表（claude-code、codex、zcode、kimi、pi、opencode、omp）且 provider 档案存在。切换 SHALL 把 provider 指向、**Agent 模型集**（默认取 Provider 模型集全集，显式给出时取保序子集）与**默认模型**写入该 agent 的原生配置，使该 agent 自己的模型选择器能在集合内切换：claude-code 写 `modelPicker`（每行 SHALL 带 `behavesAs`，映射到该版本已知模型或其 1M 形态），codex 写指向 senv 生成 catalog 文件的 `model_catalog_json`，kimi 为每个模型写一条 `[models.*]`，pi 写 `providers.<id>.models[]` 并在既有 `enabledModels` 非空时把默认模型 scope 置顶，opencode 写 `provider.<id>.models{}`，omp 写 `models.yml` 的 `providers.<id>`（条目形状与投影同 pi）并在 `config.yml` 写 `modelRoles.default` 为 `<id>/<默认模型>`。Agent 模型集 MUST NOT 为空，每个模型 MUST 属于档案模型集；默认模型 MUST 属于 Agent 模型集。模型元数据 SHALL 优先取档案内的 per-model 信息，缺失字段才回退 models.dev 缓存；旧档案没有该信息时 MUST 保持可切换，MUST NOT 回写档案。已解析的元数据 SHALL 按各 agent 原生字段投影，避免落到 agent 内置默认：context window 写 pi/omp `contextWindow`、opencode `limit.context`、kimi `max_context_size`、codex catalog `context_window`；输出上限写 pi/omp `maxTokens`、opencode `limit.output`、kimi `max_output_size`；推理档位非空时写 pi/omp/opencode `reasoning: true`、kimi `capabilities` 含 `thinking` 与 `support_efforts`、codex catalog `supported_reasoning_levels`；已声明的默认推理档写 codex `default_reasoning_level`（MUST 属于档位列表）；输入模态写 codex `input_modalities`、kimi `capabilities` 的 `image_in`/`video_in`（当模态含 image/video）、pi/omp `input`、opencode `modalities.input`。某模型缺失某项元数据时该字段 MUST 省略而不是写 0/false/空数组，但 codex catalog 的推理档位与输入模态除外：无档位或旧档案缺默认推理档时 MUST 写入单档 `none` 并设 `default_reasoning_level` 为 `none`（MUST NOT 取档位列表首项）；缺输入模态时 MUST 写入 `["text"]`。这两处是 agent 投影模板，不是 senv 对模型事实的推断。空的 `supported_reasoning_levels` 会使 Codex 会话内 `/model` 选择器无法关闭。切换 SHALL 清理上一次由 senv 写入、本次不再需要的条目与不再被指向的 `senv-<alias>` catalog 文件。切换前 SHALL 校验档案 `api_shape`（若声明）与目标 agent 协议族的兼容性，不兼容时 MUST 拒绝且不写任何文件；协议族内兼容时 SHALL 把声明值投影为该 agent 的线协议字段：pi/omp `api` 写 `openai-responses`、kimi provider `type` 写 `openai_responses`、codex `wire_api` 恒写 `responses`（上游已移除 chat 线协议，声明不再改变它）、opencode npm 写 `@ai-sdk/openai`（声明 `openai-responses`）；未声明 `api_shape` 时各字段 SHALL 维持既有默认（pi/omp `openai-completions`、kimi `openai`、codex `responses`、opencode `@ai-sdk/openai-compatible`）。档案声明 `openai-chat` 且无 `responses_base_url` 时 MUST 拒绝切换 codex 且不写任何文件（codex 只讲 Responses 线协议）；设有 `responses_base_url` 时放行，地址取该字段。校验通过后 SHALL 解密凭据引用并调用该 agent 的适配器写回配置，再更新本机指针。对已指向同一 provider 的 agent，以新默认模型重跑切换 SHALL 仅更换默认模型，Agent 模型集、provider 指向与配置中的其它字段保持不变；pi 为让默认模型在非空 `enabledModels` 中优先生效而重排 senv scope 时，用户其他 scope 不受影响；omp 仅更新 `modelRoles.default`。以显式模型集重跑 SHALL 按新集合重算并清理差集。TUI SHALL 提供等价的「仅换默认模型」入口。

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
- **THEN** pi/omp provider `api` 写 `openai-responses`，kimi provider `type` 写 `openai_responses`，opencode provider `npm` 写 `@ai-sdk/openai`；codex 不受此声明影响（保持 `responses`）

#### Scenario: 声明 openai-chat 时 codex 只走 responses
- **WHEN** 档案 `api_shape` 为 `openai-chat` 且用户切换 codex
- **THEN** 无 `responses_base_url` 时 MUST 拒绝且不写任何文件（codex 已移除 chat 线协议）；设有 `responses_base_url` 时 `[model_providers.<id>]` 的 `wire_api` 写 `responses` 且地址取 `responses_base_url`；未设形态地址或未声明 `api_shape` 时同样写 `responses`，地址回落推断

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
- **THEN** Codex catalog 写 `input_modalities: ["text","image"]`；kimi `capabilities` 含 `image_in`；pi/omp 条目含 `input: ["text","image"]`；opencode 条目含 `modalities.input`

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

### Requirement: 凭据按 agent 格式落地
切换时 SHALL 从 vault 解密凭据引用（`text:` 经 text manager、`env:` 经 env manager）并按 agent 原生格式写入配置：支持文件内凭据字段的 agent（claude-code、zcode、kimi、pi、omp、opencode）直接写入并收敛文件权限为 0600；codex 的 TOML 配置只写 `model`/`model_provider`/`base_url`/`env_key`，凭据 MUST NOT 写入文件。codex 的切换 SHALL 同样解密凭据引用以校验其在本机存在：引用缺失时 MUST 拒绝切换且零写入（诊断含引用的完整名字与修复指引），明文 MUST NOT 落盘、MUST NOT 进入命令输出。成功输出 SHALL 含写进 `env_key` 的确切环境变量名。

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

### Requirement: 接入地址按协议族写回

`senv ai switch` SHALL 先解析本次写回的接入地址，再按目标 agent 的协议族转换后写配置。地址来源 SHALL 为：目标 agent 协议族的显式形态地址存在时优先——Anthropic Messages 族（claude-code）用档案 `anthropic_base_url` 并原样写回（MUST NOT 剥离或改写任何路径段）；OpenAI 兼容族（codex/kimi/pi/omp/opencode）按已解析线协议取对应形态字段（线协议为 chat 取 `chat_base_url`，为 responses 取 `responses_base_url`）。目标族无显式形态地址时回落 `BaseURL` 并按协议族转换：Anthropic 族写不带版本段的形态，OpenAI 兼容族写带末段 `/v1` 的形态。转换 SHALL 在写回前完成且幂等：档案接入地址已归一或未归一的结果一致，重复切换不产生配置漂移。转换 MUST 只处理路径末段并保留 query 与 fragment；解析失败或缺少 scheme/host 时 SHALL 原样写回，由既有校验路径报错。命令成功输出 SHALL 包含实际写入的接入地址及其来源（显式形态地址字段名，或「由 BaseURL 推断」）。本要求 MUST NOT 改变档案在 vault 中的存储值，也 MUST NOT 要求迁移存量档案。

#### Scenario: claude-code 剥离版本段

- **WHEN** 档案接入地址为 `https://api.example.com/v1` 且未设置 `anthropic_base_url`，执行 `senv ai switch claude-code <provider>`
- **THEN** `ANTHROPIC_BASE_URL` 写为 `https://api.example.com`，输出显示该实际写入值与「由 BaseURL 推断」来源


#### Scenario: 显式 anthropic 形态地址原样写回

- **WHEN** 档案 `anthropic_base_url` 为 `https://gw.example.com/api/anthropic`，执行 `senv ai switch claude-code <provider>`
- **THEN** `ANTHROPIC_BASE_URL` 写为 `https://gw.example.com/api/anthropic`（原样，不剥离、不追加版本段），输出显示该值与显式来源

#### Scenario: OpenAI 族显式形态地址优先

- **WHEN** 档案声明 `api_shape=openai-chat` 且 `chat_base_url` 为 `https://chat.example.com/v1`、`BaseURL` 为 `https://gw.example.com/v1`，切换 codex
- **THEN** TOML `base_url` 写为 `https://chat.example.com/v1`，输出显示该值与显式来源

#### Scenario: OpenAI 族字段未设回落 BaseURL

- **WHEN** 档案声明 `api_shape=openai-chat` 且 `chat_base_url` 未设置，切换 codex
- **THEN** 写回 `BaseURL`（带版本段形态），输出显示来源为「由 BaseURL 推断」

#### Scenario: Anthropic 族剥离是无损变换

- **WHEN** 档案接入地址为 `https://api.example.com/v1` 或 `https://api.example.com` 且未设置形态地址
- **THEN** claude-code 最终请求的 URL 均为 `https://api.example.com/v1/messages`

#### Scenario: OpenAI 兼容族补版本段

- **WHEN** 存量档案接入地址为 `https://api.example.com`（无版本段）且执行 `senv ai switch codex <provider>`
- **THEN** TOML `base_url` 写为 `https://api.example.com/v1`，输出显示该实际写入值

#### Scenario: 带路径前缀的接入地址

- **WHEN** 档案接入地址为 `https://api.example.com/api/llm/v1` 且执行 `senv ai switch claude-code <provider>`
- **THEN** 写为 `https://api.example.com/api/llm`，中间路径段不被改动

#### Scenario: query 与 fragment 保留

- **WHEN** 档案接入地址为 `https://api.example.com/v1?key=abc`
- **THEN** OpenAI 兼容族写回后 query 仍在，Anthropic 族剥离版本段后 query 仍随 base 保留

#### Scenario: 非 v1 版本段不被猜测

- **WHEN** 档案接入地址为 `https://api.example.com/v1beta`
- **THEN** Anthropic 族原样写回，OpenAI 兼容族补为 `https://api.example.com/v1beta/v1`，不做协议探测

#### Scenario: 重复切换幂等

- **WHEN** 对同一 agent 连续执行两次相同 `switch`
- **THEN** 第二次写回的接入地址与第一次相同，配置无漂移

### Requirement: 形态地址门禁

`senv ai switch` SHALL 在写配置前判定目标 agent 协议族（Anthropic 族：claude-code；OpenAI 兼容族：codex/kimi/pi/omp/opencode）的兼容性：目标族存在显式形态地址（Anthropic 族为档案 `anthropic_base_url` 非空；OpenAI 兼容族为 `chat_base_url` 或 `responses_base_url` 任一非空）时 SHALL 放行，MUST NOT 受档案 `api_shape` 声明影响；目标族无显式形态地址时 SHALL 维持既有判定（`api_shape` 未声明放行；声明族与目标族不兼容时 MUST 拒绝且不写任何文件）。拒绝时错误信息 SHALL 给出三个可行动作：改档案形态、补配该族形态地址（`--shape-url <api_shape>=<url>`）、换 provider。形态地址的存在性 MUST NOT 反向决定 OpenAI 族内的线协议选择；线协议仍由档案 `api_shape`（openai-* 声明）或 agent 既有默认决定。MUST NOT 从 URL 内容推断形态。

#### Scenario: 显式 anthropic 地址放行 openai-chat 声明

- **WHEN** 档案 `api_shape` 为 `openai-chat` 且 `anthropic_base_url` 已设置，用户执行 `senv ai switch claude-code <provider>`
- **THEN** 切换成功，claude-code 写入 `anthropic_base_url` 的原样值


#### Scenario: 无显式地址维持拒绝

- **WHEN** 档案 `api_shape` 为 `openai-chat`、三个形态地址均未设置，用户切换 claude-code
- **THEN** 命令以非 0 退出，错误给出改档案形态、补 `--shape-url`、换 provider 三个动作，不写任何文件

#### Scenario: OpenAI 族地址存在即放行 anthropic 声明

- **WHEN** 档案 `api_shape` 为 `anthropic` 且 `responses_base_url` 已设置，用户切换 codex
- **THEN** 门禁放行，codex 线协议维持其既有默认，不因形态地址存在而改变

#### Scenario: 未声明且无显式地址维持旧行为

- **WHEN** 档案 `api_shape` 未设置且三个形态地址均未设置，切换任一 agent
- **THEN** 行为与本要求生效前一致

## ADDED Requirements

### Requirement: omp 写回契约（YAML）

切换 omp SHALL 写两个 YAML 文件：`models.yml` 的 `providers.senv-<alias>` 条目（`baseUrl`、`apiKey`、`api`、`compat` 与 `models[]`，条目形状与投影规则同 pi：context window 写 `contextWindow`、输出上限写 `maxTokens`、推理档位非空写 `reasoning: true`、输入模态写 `input`，缺失项省略）与 `config.yml` 的 `modelRoles.default`（值为 `<id>/<默认模型>` 选择子）。`config.yml` 的其它键与 `modelRoles` 的其它角色（smol/slow/plan 等）MUST 原样保留，MUST NOT 写入或清理；omp 未配置的角色回退 default，无需 senv 代管。两个文件均遵循统一写回协议：保留无关键 → 备份 → temp+rename 原子替换，失败回滚；凭据明文写入 `models.yml` 并收敛文件权限 0600。换 provider 重跑切换 SHALL 删除 `models.yml` 中上一次 senv provider 的条目并更新 `modelRoles.default`。omp 的用户级配置路径解析 SHALL 镜像 omp 自身规则（见「omp 配置路径解析」）。

#### Scenario: 切换 omp 写两个 YAML 文件

- **WHEN** 用户切换 omp 至某 provider，档案默认模型 m1、模型集 m1、m2，且 `~/.omp/agent/config.yml` 已有 `theme` 等自有键、`modelRoles` 已有 `slow: some/other`
- **THEN** `models.yml` 写出 `providers.senv-<alias>`（含 baseUrl、apiKey、api、models[] 两条）且两个文件权限 0600；`config.yml` 的 `modelRoles.default` 为 `senv-<alias>/m1`，`modelRoles.slow` 与 `theme` 等其它键原样保留

#### Scenario: 换 provider 清理旧条目

- **WHEN** omp 当前指向 A（默认模型 m1），用户切到 B（默认模型 m2）
- **THEN** `models.yml` 中 `providers.senv-a` 被删除、`providers.senv-b` 写入；`modelRoles.default` 变为 `senv-b/m2`

#### Scenario: 以新默认模型重跑仅改 default

- **WHEN** omp 当前指向 A 且 Agent 模型集为 m1、m2，用户以默认模型 m2 重跑切换
- **THEN** `models.yml` 的 provider 条目与模型集不变，仅 `modelRoles.default` 变为 `senv-a/m2`

### Requirement: omp 配置路径解析

omp 的用户级配置目录 SHALL 按 omp 自身规则解析：默认 `~/.omp/agent`；`OMP_PROFILE`（优先）或 `PI_PROFILE` 激活有名 profile 时解析为 `~/.omp/profiles/<name>/agent`；profile 名非法（不匹配 omp 的名字规则）时回退默认 profile（与 omp 的 `readProfileFromEnvSafe` 一致），MUST NOT 报错；`PI_CONFIG_DIR` 改写配置根；`PI_CODING_AGENT_DIR` 仅默认 profile 时改写 agent 目录，profile 激活时 MUST 忽略它；两者的值按 omp 的字面语义使用——omp 经 Node `path.join`/`path.resolve` 解析，不展开 `~`，相对路径锚定进程工作目录，senv  MUST 镜像而不是另造更友好但 omp 不会读的语义；Linux 的 XDG 重定向 MUST NOT 支持（omp 侧要求先 `omp config migrate`，代码注释说明）。

#### Scenario: 默认路径

- **WHEN** 未设置任何相关环境变量，用户切换 omp 或 `senv mcp install omp`
- **THEN** 目标路径解析到 `~/.omp/agent/` 下的 models.yml / config.yml / mcp.json

#### Scenario: profile 激活时整体迁移

- **WHEN** `OMP_PROFILE=work` 已设置，用户切换 omp
- **THEN** 目标路径解析到 `~/.omp/profiles/work/agent/` 下的 models.yml 与 config.yml

#### Scenario: profile 激活时忽略 PI_CODING_AGENT_DIR

- **WHEN** `OMP_PROFILE=work` 与 `PI_CODING_AGENT_DIR=~/custom` 同时设置，用户切换 omp
- **THEN** 目标路径仍解析到 `~/.omp/profiles/work/agent/`，`PI_CODING_AGENT_DIR` 被忽略

#### Scenario: 默认 profile 尊重 PI_CODING_AGENT_DIR

- **WHEN** 仅设置 `PI_CODING_AGENT_DIR=~/custom`（无 profile），用户切换 omp
- **THEN** 目标路径解析到 `~/custom/` 下的 models.yml 与 config.yml

### Requirement: agent id 别名查找

`mcp` 与 `llm` 两个注册表的 id 查找 SHALL 接受各目标声明的别名（omp 的别名为 `oh-my-pi`），大小写不敏感；别名 MUST NOT 出现在任何列表、`ai status` 输出、报错枚举或帮助文本中——展示面只显示规范 id。

#### Scenario: 别名可查找

- **WHEN** 用户执行 `senv ai switch oh-my-pi myprovider` 或 `senv mcp install oh-my-pi`
- **THEN** 两条命令都按 omp 处理，写盘目标与 `omp` 完全一致

#### Scenario: 展示面只有规范 id

- **WHEN** 用户执行 `senv ai status` 或 `senv mcp install`（无参数列表）
- **THEN** 输出中只出现 `omp`，不出现 `oh-my-pi`

### Requirement: omp 兼容字段投影

切换 omp 写 `models.yml` 的 provider 定义时 SHALL 一并写 provider 级 `compat`，其中 `supportsDeveloperRole` MUST 为 `false`。理由与 pi 相同（见「pi 兼容字段投影」）：omp 沿用 pi 的判定——`model.reasoning && compat.supportsDeveloperRole` 决定 system prompt 用 `developer` 还是 `system` 角色，缺省推断只认硬编码的 base URL/provider 特征名单，senv 写入的自建网关会被误判，使推理模型发出上游不接受的 `developer` 角色。该声明 MUST 覆盖该 provider 下全部模型，MUST NOT 出现 `supportsReasoningEffort: false`。

#### Scenario: 推理模型不发 developer 角色

- **WHEN** 用户切换 omp 至某 provider，其档案为模型 m1 保存了推理档位，接入地址为自建网关
- **THEN** `providers.<id>` 含 `compat.supportsDeveloperRole: false`，m1 条目仍含 `reasoning: true`，首条消息使用 `system` 角色

#### Scenario: 不连带关闭推理档位透传

- **WHEN** 切换 omp 至任一 provider
- **THEN** provider `compat` 中 MUST NOT 出现 `supportsReasoningEffort: false`
