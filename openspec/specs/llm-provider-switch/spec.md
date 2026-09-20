# llm-provider-switch Specification

## Purpose
把「切换 coding agent 的 LLM Provider」从手工改配置文件变成一条 senv 命令：以本机指针记录每个 agent 当前指向，切换时从 vault 解密凭据并按 agent 原生格式原子写回配置，失败不留半写状态。

## Requirements

### Requirement: 切换 agent 指向
`senv ai switch <agent> <provider>` SHALL 校验 agent 属于支持的注册表（claude-code、codex、zcode、kimi、pi、opencode）且 provider 档案存在。切换 SHALL 把 provider 指向、**Agent 模型集**（默认取 Provider 模型集全集，显式给出时取保序子集）与**默认模型**写入该 agent 的原生配置，使该 agent 自己的模型选择器能在集合内切换：claude-code 写 `modelPicker`（每行 SHALL 带 `behavesAs`，映射到该版本已知模型或其 1M 形态），codex 写指向 senv 生成 catalog 文件的 `model_catalog_json`，kimi 为每个模型写一条 `[models.*]`，pi 写 `providers.<id>.models[]` 并在既有 `enabledModels` 非空时把默认模型 scope 置顶，opencode 写 `provider.<id>.models{}`。Agent 模型集 MUST NOT 为空，每个模型 MUST 属于档案模型集；默认模型 MUST 属于 Agent 模型集。模型元数据 SHALL 优先取档案内的 per-model 信息，缺失字段才回退 models.dev 缓存；旧档案没有该信息时 MUST 保持可切换，MUST NOT 回写档案。已解析的元数据 SHALL 按各 agent 原生字段投影，避免落到 agent 内置默认：context window 写 pi `contextWindow`、opencode `limit.context`、kimi `max_context_size`、codex catalog `context_window`；输出上限写 pi `maxTokens`、opencode `limit.output`、kimi `max_output_size`；推理档位非空时写 pi/opencode `reasoning: true`、kimi `capabilities` 含 `thinking` 与 `support_efforts`、codex catalog `supported_reasoning_levels`；已声明的默认推理档写 codex `default_reasoning_level`（MUST 属于档位列表）；输入模态写 codex `input_modalities`、kimi `capabilities` 的 `image_in`/`video_in`（当模态含 image/video）、pi `input`、opencode `modalities.input`。某模型缺失某项元数据时该字段 MUST 省略而不是写 0/false/空数组，但 codex catalog 的推理档位与输入模态除外：无档位或旧档案缺默认推理档时 MUST 写入单档 `none` 并设 `default_reasoning_level` 为 `none`（MUST NOT 取档位列表首项）；缺输入模态时 MUST 写入 `["text"]`。这两处是 agent 投影模板，不是 senv 对模型事实的推断。空的 `supported_reasoning_levels` 会使 Codex 会话内 `/model` 选择器无法关闭。切换 SHALL 清理上一次由 senv 写入、本次不再需要的条目与不再被指向的 `senv-<alias>` catalog 文件。切换前 SHALL 校验档案 `api_shape`（若声明）与目标 agent 协议族的兼容性，不兼容时 MUST 拒绝且不写任何文件；协议族内兼容时 SHALL 把声明值投影为该 agent 的线协议字段：pi `api` 写 `openai-responses`、kimi provider `type` 写 `openai_responses`、codex `wire_api` 恒写 `responses`（上游已移除 chat 线协议，声明不再改变它）、opencode npm 写 `@ai-sdk/openai`（声明 `openai-responses`）；未声明 `api_shape` 时各字段 SHALL 维持既有默认（pi `openai-completions`、kimi `openai`、codex `responses`、opencode `@ai-sdk/openai-compatible`）。档案声明 `openai-chat` 且无 `responses_base_url` 时 MUST 拒绝切换 codex 且不写任何文件（codex 只讲 Responses 线协议）；设有 `responses_base_url` 时放行，地址取该字段。校验通过后 SHALL 解密凭据引用并调用该 agent 的适配器写回配置，再更新本机指针。对已指向同一 provider 的 agent，以新默认模型重跑切换 SHALL 仅更换默认模型，Agent 模型集、provider 指向与配置中的其它字段保持不变；pi 为让默认模型在非空 `enabledModels` 中优先生效而重排 senv scope 时，用户其他 scope 不受影响。以显式模型集重跑 SHALL 按新集合重算并清理差集。TUI SHALL 提供等价的「仅换默认模型」入口。

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
切换时 SHALL 从 vault 解密凭据引用（`text:` 经 text manager、`env:` 经 env manager）并按 agent 原生格式写入配置：支持文件内凭据字段的 agent（claude-code、zcode、kimi、pi、opencode）直接写入并收敛文件权限为 0600；codex 的 TOML 配置只写 `model`/`model_provider`/`base_url`/`env_key`，凭据 MUST NOT 写入文件。codex 的切换 SHALL 同样解密凭据引用以校验其在本机存在：引用缺失时 MUST 拒绝切换且零写入（诊断含引用的完整名字与修复指引），明文 MUST NOT 落盘、MUST NOT 进入命令输出。成功输出 SHALL 含写进 `env_key` 的确切环境变量名。

#### Scenario: 文件内凭据 agent
- **WHEN** 切换 pi 至某 provider
- **THEN** `~/.pi/agent/models.json` 中对应 provider 含解密后的 apiKey，文件权限为 0600

#### Scenario: codex 凭据不落盘
- **WHEN** 切换 codex 至某 provider
- **THEN** `config.toml` 含模型与 provider 定义及 `env_key` 名，不含任何密钥明文

#### Scenario: codex 凭据引用缺失
- **WHEN** 档案的 `credential_ref` 指向本机 vault 中不存在的条目且切换 codex
- **THEN** 切换以非零退出、错误含该引用的完整名字与补齐指引、`config.toml` 零写入

### Requirement: codex 凭据暴露名可被 senv env 导出
`senv ai switch codex` 写入 `env_key` 的环境变量名 SHALL 保证可由 `senv env export` 提供，使 codex 无需任何额外手工步骤即可取到凭据：

- `credential_ref` 为 `env:<g>/<k>` 时，`env_key` SHALL 为 `<k>`（即该条目在 shell 中的既有名字），且切换 MUST NOT 新增或修改任何 env 条目。
- `credential_ref` 为 `text:<g>/<k>`（含 alias 规范引用 `text:llm-keys/<alias>`）时，`env_key` SHALL 为 `SENV_<alias 净化并大写>_API_KEY`，且切换 SHALL 在**默认 env 组**写入一条同名条目，其值为对 `text:<g>/<k>` 的引用（`{{text:<g>/<k>}}`），使该名字随导出解析为同一凭据。
- 该兜底写入 SHALL 幂等：目标名字已存在于任何 env 组时 MUST NOT 覆盖其值、MUST NOT 新增重复条目。
- 名字所处 env 组不在导出集合（默认组 ∪ 已激活组）内时，切换 SHALL 输出可操作 warning，指明该组与激活命令（如 `senv env group activate <g>`）。
- 命令 MUST NOT 注入进程环境、MUST NOT 引入凭据代理或启动包装。

#### Scenario: env 引用沿用既有变量名
- **WHEN** 档案 `credential_ref` 为 `env:ai/DEEPSEEK_API_KEY`（`ai` 组已激活）且切换 codex
- **THEN** `config.toml` 的 `env_key` 为 `DEEPSEEK_API_KEY`，vault 中无新增 env 条目，输出含该名字

#### Scenario: 自有凭据由默认组引用条目兜底
- **WHEN** 档案 `credential_ref` 为 `text:llm-keys/main` 且切换 codex
- **THEN** `env_key` 为 `SENV_MAIN_API_KEY`，默认 env 组出现 `SENV_MAIN_API_KEY` 且其值为 `{{text:llm-keys/main}}`，`senv env export` 输出该变量

#### Scenario: 不覆盖既有同名条目
- **WHEN** 默认 env 组已存在用户自己设置的 `SENV_MAIN_API_KEY` 且档案 `credential_ref` 为 `text:llm-keys/main` 时切换 codex
- **THEN** 该条目值与数量均不变，切换不报错，输出仍含最终 `env_key` 名

#### Scenario: 引用组未激活时提示
- **WHEN** 档案 `credential_ref` 为 `env:dev/APP_KEY` 且 `dev` 组未激活时切换 codex
- **THEN** 切换成功且输出 warning，指出导出不会包含该名字并给出 `senv env group activate dev`

### Requirement: 切换需要解锁 vault
`senv ai switch` SHALL 在 vault 未初始化或未解锁时遵循既有认证流程（提示口令）；指针读取与 `senv ai status` 不需要 vault。

#### Scenario: 锁定状态执行 switch
- **WHEN** vault 已初始化但锁定且用户执行 switch
- **THEN** 提示输入口令，认证通过后完成切换

#### Scenario: status 无需解锁
- **WHEN** vault 锁定且用户执行 status
- **THEN** 正常显示指针状态，不提示口令

### Requirement: 接入地址按协议族写回

`senv ai switch` SHALL 先解析本次写回的接入地址，再按目标 agent 的协议族转换后写配置。地址来源 SHALL 为：目标 agent 协议族的显式形态地址存在时优先——Anthropic Messages 族（claude-code）用档案 `anthropic_base_url` 并原样写回（MUST NOT 剥离或改写任何路径段）；OpenAI 兼容族（codex/kimi/pi/opencode）按已解析线协议取对应形态字段（线协议为 chat 取 `chat_base_url`，为 responses 取 `responses_base_url`）。目标族无显式形态地址时回落 `BaseURL` 并按协议族转换：Anthropic 族写不带版本段的形态，OpenAI 兼容族写带末段 `/v1` 的形态。转换 SHALL 在写回前完成且幂等：档案接入地址已归一或未归一的结果一致，重复切换不产生配置漂移。转换 MUST 只处理路径末段并保留 query 与 fragment；解析失败或缺少 scheme/host 时 SHALL 原样写回，由既有校验路径报错。命令成功输出 SHALL 包含实际写入的接入地址及其来源（显式形态地址字段名，或「由 BaseURL 推断」）。本要求 MUST NOT 改变档案在 vault 中的存储值，也 MUST NOT 要求迁移存量档案。

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

### Requirement: 切换命令参数
`senv ai switch <agent> <provider>` SHALL 接受 `--models`（逗号分隔、可重复；省略时取 Provider 模型集全集，显式给出时保序）与 `--default-model`（省略时取档案默认模型）。`--model` MUST NOT 被接受：出现时命令 MUST 以非 0 退出并提示改用 `--models` 与 `--default-model`。参数校验 MUST 在任何文件写入之前完成，失败时不改任何文件。成功输出 SHALL 包含 provider、Agent 模型集条数与默认模型。审计事件 SHALL 记录默认模型与模型集条数，且 MUST NOT 包含任何凭据材料或值。

#### Scenario: 省略 --models 即全选
- **WHEN** 用户执行 `senv ai switch claude-code myprovider` 且档案模型集为 m1、m2
- **THEN** 本次 Agent 模型集为 m1、m2，输出显示条数为 2 与默认模型

#### Scenario: --models 显式给定保序
- **WHEN** 用户执行 `--models m2,m1`
- **THEN** Agent 模型集按 m2、m1 的顺序写入

#### Scenario: --model 被拒绝并提示
- **WHEN** 用户执行 `senv ai switch claude-code myprovider --model m1`
- **THEN** 命令以非 0 退出，提示改用 `--models` 与 `--default-model`，不写任何文件

#### Scenario: --default-model 覆盖本次
- **WHEN** 档案默认模型为 m1，用户执行 `--default-model m2`（m2 在 Agent 模型集内）
- **THEN** 本次默认模型为 m2，档案的默认模型保持不变

#### Scenario: 审计记录模型数与默认模型
- **WHEN** 切换成功
- **THEN** 审计事件 detail 含默认模型与模型集条数，不含凭据材料

### Requirement: Provider 重命名更新本机指针
当用户执行 LLM Provider rename 时，本机 `agent-pointers` 中所有指向旧别名的条目 SHALL 将 `provider` 字段更新为新别名；Agent 模型集与默认模型 MUST 保持不变。rename MUST NOT 改写各 coding agent 的原生配置文件，也 MUST NOT 重命名或删除 `senv-<old>` catalog 等由既往 `switch` 产生的派生文件。此后 `senv ai status` SHALL 显示新别名；若原生配置仍含 `senv-<old>`，用户重跑 `senv ai switch <agent> <new>` 后 SHALL 按既有 switch 语义写出 `senv-<new>` 并清理不再被指向的旧 catalog（与缩集/换 provider 的清理规则一致）。指针文件缺失或为空时 rename 的档案/凭据步骤仍可成功，受影响指针数记为 0。

#### Scenario: 指针随 rename 更新
- **WHEN** codex 与 pi 的指针均指向 `acme`，执行 provider rename `acme` → `acme-prod`
- **THEN** 两处指针的 provider 均为 `acme-prod`，模型集与默认模型不变；codex/pi 原生配置文件内容不变

#### Scenario: 重切写出新标识并清理旧 catalog
- **WHEN** rename 后用户对仍含 `senv-acme` 的 agent 执行 `senv ai switch <agent> acme-prod`
- **THEN** 原生配置与 catalog 使用 `senv-acme-prod`；若不再有任何指针引用 `acme`，既有 switch 清理规则删除不再需要的 `senv-acme` catalog

#### Scenario: 无指针文件仍可 rename
- **WHEN** 本机尚无 agent 指针文件，执行 rename
- **THEN** 档案（及适用时的自有凭据）改名成功，受影响指针数为 0

### Requirement: 形态地址门禁

`senv ai switch` SHALL 在写配置前判定目标 agent 协议族（Anthropic 族：claude-code；OpenAI 兼容族：codex/kimi/pi/opencode）的兼容性：目标族存在显式形态地址（Anthropic 族为档案 `anthropic_base_url` 非空；OpenAI 兼容族为 `chat_base_url` 或 `responses_base_url` 任一非空）时 SHALL 放行，MUST NOT 受档案 `api_shape` 声明影响；目标族无显式形态地址时 SHALL 维持既有判定（`api_shape` 未声明放行；声明族与目标族不兼容时 MUST 拒绝且不写任何文件）。拒绝时错误信息 SHALL 给出三个可行动作：改档案形态、补配该族形态地址（`--shape-url <api_shape>=<url>`）、换 provider。形态地址的存在性 MUST NOT 反向决定 OpenAI 族内的线协议选择；线协议仍由档案 `api_shape`（openai-* 声明）或 agent 既有默认决定。MUST NOT 从 URL 内容推断形态。

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
