# llm-provider Specification

## Purpose
把 LLM Provider 档案作为 vault 加密资产存储：档案与凭据分离（档案只存凭据引用），模型集可从模型目录自动装配并允许自定义，为后续 agent 切换提供唯一事实源。

## Requirements

### Requirement: 添加 LLM Provider 档案
`senv ai provider add` SHALL 以别名为唯一标识保存档案。凭据必须且只能通过交互式 TTY prompt 或 `--api-key-stdin` 提供；prompt 或 stdin 值 SHALL 只进入 vault 保留 text 组 `llm-keys/<alias>`，档案存引用 `text:llm-keys/<alias>`，且 MUST NOT 作为 CLI flag 值传入。也可用 `--key-ref` 指向既有 env/text entry（`env:<group>/<key>` 或 `text:<group>/<key>`）。base URL SHALL 默认仅接受 HTTPS；显式 `--allow-http` 后接受 HTTP；两种方案 MUST 拒绝 host 为空或包含 userinfo。base URL 落库前 SHALL 归一为 OpenAI 兼容形态：收敛路径尾斜杠，并在路径末段不为 `v1` 时追加 `/v1`；归一 MUST 保留 query 与 fragment，MUST NOT 改动中间路径段，且对已归一的输入保持原值。归一实际改动了输入时命令 SHALL 提示改写后的值。归一 MUST NOT 放宽既有校验：userinfo、空 host 与非允许的 HTTP 仍被拒绝。模型集为 `--catalog-provider` 指向的目录模型与 `--model` 自定义模型的并集且不得为空；每个模型 SHALL 解析 context window：显式 `--model-context <model>=<tokens>` 优先，其次档案既有元数据，最后 models.dev `limit.context`。add 时任一模型缺失 context window MUST 以非 0 退出且不写档案或凭据。模型元数据还 SHALL 支持显式设置输出上限、推理档位、默认推理档与输入模态：重复的 `--model-output <model>=<tokens>`、`--model-reasoning <model>=<effort>[;<effort>...]`（档位以分号分隔）、`--model-default-reasoning <model>=<effort>`、`--model-modalities <model>=<mod>[;<mod>...]`（模态以分号或逗号分隔），以及集合级 `--default-reasoning <effort>`。显式 per-model 值优先于档案既有元数据、集合级声明与目录；集合级 `--default-reasoning` 只填充「声明了推理档位且尚未解析出默认推理档」的模型，MUST NOT 盖到无档位模型上。senv MUST NOT 从档位列表形状或模型 id 推断默认推理档或输入模态。凡最终解析出非空推理档位的模型 MUST 再解析出默认推理档，且该值 MUST 属于其档位列表；目录当前无 default effort 字段时须由显式 per-model 或集合级声明提供。无档位的模型 MUST NOT 要求默认推理档。输入模态可选：目录 `modalities.input` 或显式值有则写入，缺席表示未知，MUST NOT 写成纯文本。不在最终模型集内的模型、非正整数输出上限、空档位、默认推理档不属于档位列表、非法模态名 MUST 以非 0 退出且不写档案或凭据。`--default-model` 必须属于模型集。`--force` 覆盖档案时：未提供新自有凭据 SHALL 保留既有凭据与引用；从自有凭据改为外部引用 SHALL 删除原自有凭据；提供新自有凭据 SHALL 覆盖旧自有凭据。

#### Scenario: 交互凭据不进 argv
- **WHEN** 用户在 TTY 执行 add 且按提示输入 API key
- **THEN** key 存入 vault，进程 argv、成功输出、list/show/MCP/TUI 均不含 key 明文

#### Scenario: stdin 凭据用于脚本
- **WHEN** 用户执行 `printf '%s' "$KEY" | senv ai provider add local --base-url ... --api-key-stdin ...`
- **THEN** key 从 stdin 读取并存入 vault，`--api-key` flag 不存在

#### Scenario: HTTP 需要显式允许
- **WHEN** 用户添加 `http://localhost:11434/v1` 且未提供 `--allow-http`
- **THEN** 命令非 0 退出并提示 HTTPS 是默认要求，不写档案或凭据

#### Scenario: 拒绝 URL userinfo
- **WHEN** base URL 为 `https://user:pass@example.com/v1`
- **THEN** 命令非 0 退出，档案不保存 userinfo，后续输出不泄露凭据

#### Scenario: 缺版本段的 base URL 被归一
- **WHEN** 用户添加 `--base-url https://api.example.com` 且未提供 `--allow-http`
- **THEN** 档案保存 `https://api.example.com/v1`，命令提示该接入地址已被规范化

#### Scenario: 尾斜杠被收敛
- **WHEN** 用户添加 `--base-url https://api.example.com/v1/`
- **THEN** 档案保存 `https://api.example.com/v1`

#### Scenario: 已归一的 base URL 静默通过
- **WHEN** 用户添加 `--base-url https://api.example.com/v1`
- **THEN** 档案保存原值，命令不输出规范化提示

#### Scenario: force 保留既有自有凭据
- **WHEN** 已存在 alias 的自有凭据且用户用 `--force` 只更新模型或默认模型，不提供新凭据来源
- **THEN** 档案更新且原 vault 凭据保持不变

#### Scenario: force 改外部引用清理自有凭据
- **WHEN** 已存在自有凭据且用户用 `--force --key-ref env:llm/KEY` 更新档案
- **THEN** 档案引用外部 key，原 `llm-keys/<alias>` 条目被删除

#### Scenario: 目录模型加自定义模型
- **WHEN** 用户执行 add 且给出 `--catalog-provider p1`、`--model custom-1`、`--model-context custom-1=1000000`，缓存中 p1 的 a、b 均有 `limit.context` 且无推理档位
- **THEN** 档案保存成功，模型集为 a、b、custom-1，每个模型保存 context window/可用元数据，凭据按所选方式存入或引用，输出保存结果

#### Scenario: 模型缺少 context window
- **WHEN** add 的自定义模型没有 `--model-context`，且目录也没有对应 `limit.context`
- **THEN** 命令以非 0 退出并提示 `--model-context <model>=<tokens>`，不写档案或凭据

#### Scenario: 显式设置输出上限与推理档位
- **WHEN** 用户执行 add 并提供 `--model-output custom-1=32000 --model-reasoning custom-1=low;high --model-default-reasoning custom-1=high`
- **THEN** 档案保存该模型的输出上限、推理档位与默认推理档，`show` 的模型信息包含这三项

#### Scenario: 有档位但缺少默认推理档
- **WHEN** add 为某模型提供了 `--model-reasoning m1=low;high`，未提供 `--model-default-reasoning` / `--default-reasoning`，且目录也没有 default effort
- **THEN** 命令以非 0 退出并提示需声明默认推理档，不写档案或凭据

#### Scenario: 集合级默认推理档填充有档位模型
- **WHEN** add 的 m1 有推理档位 `low;high`、m2 无档位，用户提供 `--default-reasoning high` 且未给 per-model 默认档
- **THEN** 档案保存 m1 的默认推理档为 `high`，m2 不写入默认推理档

#### Scenario: 默认推理档必须属于档位列表
- **WHEN** add 的 m1 档位为 `low;high` 且 `--model-default-reasoning m1=xhigh`
- **THEN** 命令以非 0 退出并提示该值不属于档位列表，不写档案或凭据

#### Scenario: 显式输入模态
- **WHEN** 用户执行 add 并提供 `--model-modalities m1=text,image`
- **THEN** 档案保存 m1 的输入模态为 text 与 image，`show` 展示该项

#### Scenario: 无档位不要求默认推理档
- **WHEN** add 的自定义模型只有 `--model-context custom-1=1000000`，未提供推理档位
- **THEN** 档案保存成功，该模型无默认推理档字段

#### Scenario: 模型信息引用了模型集外的模型
- **WHEN** add 的 `--model-output`、`--model-reasoning`、`--model-default-reasoning` 或 `--model-modalities` 指向不在最终模型集内的模型
- **THEN** 命令以非 0 退出并提示该 flag 与模型名，不写档案或凭据

#### Scenario: 目录中无该 provider
- **WHEN** `--catalog-provider` 在模型目录缓存中不存在
- **THEN** 命令以非 0 退出并提示执行 `senv ai refresh`，不写入任何档案或凭据

#### Scenario: 模型集为空
- **WHEN** add 未提供任何模型来源或并集为空
- **THEN** 命令以非 0 退出并报错，不写入任何档案或凭据

#### Scenario: 默认模型不在模型集
- **WHEN** `--default-model` 不在最终模型集中
- **THEN** 命令以非 0 退出并报错，不写入任何档案或凭据

#### Scenario: 别名重复
- **WHEN** 别名已存在且未加 `--force`
- **THEN** 命令以非 0 退出并提示使用 `--force`；加 `--force` 时按凭据覆盖语义更新档案

### Requirement: 查看 LLM Provider 档案
`senv ai provider list` SHALL 列出全部档案的摘要（别名、base_url、模型数、默认模型、目录来源、凭据引用）；`senv ai provider show` SHALL 展示单个档案详情，并在已保存模型元数据时展示各模型的 context window、输出上限、推理档位、默认推理档与输入模态。两者 MUST NOT 输出凭据明文。

#### Scenario: 列出档案
- **WHEN** vault 中存在档案且用户执行 list
- **THEN** 输出每个档案的摘要行且不含凭据明文

#### Scenario: 查看不存在的档案
- **WHEN** `show` 的别名不存在
- **THEN** 命令以非 0 退出并报错

#### Scenario: show 展示默认推理档与输入模态
- **WHEN** 档案为 m1 保存了默认推理档 `high` 与输入模态 `text,image` 且用户执行 show
- **THEN** 输出包含这两项，不含凭据明文

### Requirement: 删除 LLM Provider 档案
`senv ai provider remove` SHALL 先处理自有凭据，再删除档案：自有凭据删除失败时 SHALL 保留档案并返回错误；自有凭据不存在时 SHALL 继续删除档案；自有凭据删除成功后档案删除失败 SHALL 返回错误并说明凭据已删除。外部引用 SHALL 保留不动。

#### Scenario: 删除带自有凭据的档案
- **WHEN** 档案凭据为 `text:llm-keys/<alias>` 且凭据与档案删除均成功
- **THEN** 凭据条目先删除，档案随后删除，输出两者均已删除

#### Scenario: 自有凭据缺失不阻塞档案删除
- **WHEN** 档案引用 `text:llm-keys/<alias>` 但该 vault 条目已不存在
- **THEN** 档案删除成功，输出说明自有凭据已不存在

#### Scenario: 凭据删除失败保留档案
- **WHEN** 删除自有凭据时 vault 写操作失败
- **THEN** 档案保持存在，命令以非 0 退出并说明可重试

#### Scenario: 删除引用外部凭据的档案
- **WHEN** 档案凭据为外部 `--key-ref` 且执行 remove
- **THEN** 仅删除档案，外部条目保留，输出说明

### Requirement: 重命名 LLM Provider 档案
系统 SHALL 提供 `senv ai provider rename <old> <new>`。新别名校验 SHALL 与 `add` 一致（非空、无控制字符等既有 vault 身份规则）。目标别名已存在时 MUST 拒绝且不做任何写入。旧别名不存在时 MUST 以非 0 退出。成功时 SHALL 在同一次逻辑操作内完成：

1. 将档案主键从 `<old>` 改为 `<new>`（内容除 alias / `credential_ref` / 时间戳外 MUST 保持不变）；
2. 若档案凭据为规范自有引用 `text:llm-keys/<old>`：将 text 条目改名为 `llm-keys/<new>`（值不变），并把档案 `credential_ref` 更新为 `text:llm-keys/<new>`；目标 `llm-keys/<new>` 已存在时 MUST 拒绝整次 rename；自有凭据条目缺失时 SHALL 仍完成档案改名，并将 `credential_ref` 更新为新规范引用（与 remove 对缺失自有凭据的宽松一致）；
3. 若凭据为外部引用（非 `text:llm-keys/<old>`）：MUST NOT 移动或删除外部条目，档案 `credential_ref` 保持不变；
4. 本机 agent 指针文件中所有 `provider == <old>` 的条目 SHALL 更新为 `<new>`；模型集与默认模型字段保持不变；
5. 若步骤 2 改写了自有凭据 text 键名：SHALL 扫描 env 全部分组，将值**精确等于** `{{text:llm-keys/<old>}}` 的条目改写为 `{{text:llm-keys/<new>}}`（只改引用模板，不改 env key 名、不改其它值）；任一条目改写失败 MUST 回滚整次 rename。

任一步失败 MUST NOT 留下「档案已改名但凭据未迁」「凭据已迁但 env 引用仍指向旧 text 键」或「凭据已迁但档案仍旧名」的可观察中间态。命令输出 SHALL 包含受影响的 agent 指针数量与已更新的 env 引用条目数，并提示 coding agent 原生配置仍使用 `senv-<old>` 标识，需重跑 `senv ai switch` 才会写入 `senv-<new>`。rename MUST NOT 改写任何 coding agent 原生配置文件或 catalog 文件。输出与审计 MUST NOT 含凭据明文。TUI SHALL 提供等价操作。

#### Scenario: 重命名并联动自有凭据与指针
- **WHEN** 档案 `acme` 凭据为 `text:llm-keys/acme`，且本机有 2 个 agent 指针指向 `acme`，执行 `senv ai provider rename acme acme-prod`
- **THEN** 档案别名为 `acme-prod`，凭据条目为 `text:llm-keys/acme-prod` 且值不变，两处指针的 provider 为 `acme-prod`，输出提示 2 个指针已更新并提醒重跑 switch；agent 原生配置文件内容不变

#### Scenario: 重命名级联 env SeedRef
- **WHEN** 档案 `TokenApi` 凭据为 `text:llm-keys/TokenApi`，default 组存在 `SENV_TOKENAPI_API_KEY={{text:llm-keys:TokenApi}}`（由 codex switch 写入），执行 rename 到 `main`
- **THEN** 凭据变为 `text:llm-keys/main`，该 env 条目值变为 `{{text:llm-keys:main}}`，env key 名仍为 `SENV_TOKENAPI_API_KEY`，输出说明已更新 1 条 env 引用

#### Scenario: 外部引用不移动凭据
- **WHEN** 档案 `acme` 凭据为 `env:llm/KEY`，执行 rename 到 `acme-prod`
- **THEN** 档案改名成功，`credential_ref` 仍为 `env:llm/KEY`，`env:llm/KEY` 条目保留，不创建或删除 `llm-keys/*`，不扫描改写 env 引用

#### Scenario: 目标别名冲突
- **WHEN** 目标别名已存在
- **THEN** 命令非 0 退出，原档案、凭据、env 引用与指针全部不变

#### Scenario: 目标自有凭据键冲突
- **WHEN** 档案为自有凭据且 vault 中已存在 `text:llm-keys/<new>`（与源档案无关）
- **THEN** 命令非 0 退出，原档案与源自有凭据不变

#### Scenario: 自有凭据缺失仍可改名
- **WHEN** 档案引用 `text:llm-keys/acme` 但该 text 条目已不存在，执行 rename 到 `acme-prod`
- **THEN** 档案改名成功，`credential_ref` 变为 `text:llm-keys/acme-prod`，不因凭据缺失而失败；若 env 中存在值等于 `{{text:llm-keys/acme}}` 的条目，SHALL 改写为 `{{text:llm-keys/acme-prod}}`

#### Scenario: 旧别名不存在
- **WHEN** rename 的旧别名不在 vault
- **THEN** 命令非 0 退出且不做任何写入

#### Scenario: env 引用非精确 SeedRef 时不改写
- **WHEN** default 组存在 `NOTE=prefix {{text:llm-keys/acme}} suffix`，执行 rename `acme` → `acme-prod` 且自有凭据迁移
- **THEN** 该 env 条目值保持不变（非精确匹配），输出 env 引用更新数为 0

### Requirement: 档案命令需要解锁 vault
`senv ai provider` 子命令 SHALL 在 vault 未初始化或未解锁时遵循既有认证流程（提示口令），不静默降级为明文存储。

#### Scenario: 未解锁时执行 add
- **WHEN** vault 已初始化但处于锁定状态且用户执行 add
- **THEN** 提示输入口令，认证通过后完成写入

### Requirement: 编辑 LLM Provider 档案
`senv ai provider edit <alias>` SHALL 支持修改 base_url、模型集、模型 context window、模型输出上限、模型推理档位、默认推理档、输入模态、default_model、目录来源、凭据引用与 `api_shape`；alias 是主键 MUST NOT 被修改。凭据轮换语义 SHALL 与 `add` 一致：提供新自有凭据 SHALL 覆盖旧自有凭据；改为外部引用 SHALL 删除原自有凭据；未提供凭据来源 SHALL 保留既有凭据与引用。改动模型集、目录来源、模型 context window、推理档位或默认推理档时，必填元数据校验 SHALL 与 `add` 一致；仅修改其他字段（含只补输出上限/输入模态）时 MUST NOT 因旧档案缺少模型元数据而失败。校验（HTTPS 默认、拒绝 userinfo、模型集非空、`--default-model` 属于模型集、`--api-shape` 取值合法、默认推理档属于档位列表）SHALL 与 `add` 一致；任一步失败 MUST NOT 留下部分更新。编辑入口（CLI 与 TUI 同一方法）显式清空某个元数据字段时（默认推理档、模型输出上限、输入模态、模型 context window、模型推理档位），对应元数据 MUST 从档案中移除，MUST NOT 因档案既有元数据回填而静默保留旧值；清空后无法满足必填元数据校验的，SHALL 与 `add` 一致报错且不留下部分更新。TUI SHALL 通过同一方法提供等价编辑，别名在编辑态只读。

#### Scenario: 为旧档案补 context window
- **WHEN** 旧档案只有模型名，用户执行 `edit main --model-context m1=1000000`
- **THEN** 模型集保持不变，档案保存 m1 的 context window，后续切换无需 models.dev 缓存

#### Scenario: 为旧档案补默认推理档
- **WHEN** 旧档案 m1 已有推理档位 `low;high` 但无默认推理档，用户执行 `edit main --model-default-reasoning m1=high`
- **THEN** 模型集与档位保持不变，档案保存 m1 的默认推理档为 `high`

#### Scenario: 清空默认推理档生效
- **WHEN** 档案 m1 已保存默认推理档 `high`，用户在编辑入口清空默认推理档字段（表单置空）并提交
- **THEN** m1 的默认推理档从档案移除，推理档位与模型集保持不变，详情不再展示默认推理档

#### Scenario: 清空输出上限生效
- **WHEN** 档案 s1 已保存输出上限 32000，用户在编辑入口清空模型输出上限字段并提交
- **THEN** s1 的输出上限从档案移除，详情不再展示该项；同一次提交中对其他元数据字段的修改仍然生效

#### Scenario: 清空输入模态生效
- **WHEN** 档案 s1 已保存输入模态 `text,image`，用户在编辑入口清空输入模态字段并提交
- **THEN** s1 的输入模态从档案移除，详情不再展示该项

#### Scenario: 修改接入地址
- **WHEN** 用户执行 `senv ai provider edit main --base-url https://new.example.com/v1`
- **THEN** 档案的 base_url 更新为归一后的形态，模型集与凭据引用保持不变

#### Scenario: 轮换自有凭据
- **WHEN** 用户在 TTY 执行 edit 并输入新的 API key
- **THEN** `text:llm-keys/<alias>` 被新值覆盖，档案仍引用同一凭据条目，输出不含明文

#### Scenario: 改为外部引用
- **WHEN** 用户执行 `senv ai provider edit main --key-ref env:llm/KEY`
- **THEN** 档案改引用外部条目，原自有凭据 `text:llm-keys/main` 被删除

#### Scenario: 别名不可改
- **WHEN** 用户尝试通过 edit 改变别名
- **THEN** 命令不提供该参数并报错提示，档案与各 agent 配置中的 `senv-<alias>` 标识均不变

#### Scenario: 校验失败不留部分更新
- **WHEN** edit 提供的 `--default-model` 不属于最终模型集
- **THEN** 命令以非 0 退出，档案、凭据与引用全部保持编辑前状态

### Requirement: Provider 接入形态声明
LLM Provider SHALL 支持可选字段 `api_shape`，取值为 `openai-chat`、`openai-responses` 或 `anthropic`；`senv ai provider add/edit` SHALL 通过 `--api-shape` 设置，`--api-shape ""` SHALL 清除该字段。字段为空时 SHALL 沿用既有行为：不推断，写配置时按目标 agent 的协议族归一接入地址。字段非空时 SHALL 以其为准完成归一，`list`/`show`/TUI 详情 SHALL 展示该值（空值显示为 `-`）。本要求 MUST NOT 改变接入地址在 vault 中的存储形态与归一函数本身。

#### Scenario: 缺省不推断
- **WHEN** 档案未设置 `api_shape` 且用户切换到 claude-code
- **THEN** 写回的接入地址按 Anthropic 族剥离末段 `/v1`，行为与加此字段前一致

#### Scenario: 显式形态优先
- **WHEN** 档案 `api_shape` 为 `anthropic` 且用户切换到 claude-code
- **THEN** 归一按 Anthropic 族执行，输出显示实际写入的接入地址

#### Scenario: 清除字段回到推断
- **WHEN** 用户执行 `senv ai provider edit main --api-shape ""`
- **THEN** 档案不再带该字段，后续切换恢复按 agent 协议族推断，TUI 详情显示 `-`

#### Scenario: 非法取值
- **WHEN** 用户传入 `--api-shape openai`
- **THEN** 命令以非 0 退出并列出合法取值，不写入任何变更

### Requirement: LLM Provider 档案说明
`senv ai provider add` 与 `edit` SHALL 接受可选 `--description`。`list`/`show` SHALL 展示档案说明。说明属于 Provider 档案，MUST NOT 写入或覆盖 `model_info` 中的模型目录文案。空说明合法；长度遵循 vault-description。

#### Scenario: add with description
- **WHEN** 用户 `senv ai provider add api --description "公网 token 网关" ...`（其余必填项合法）
- **THEN** 档案保存该说明，`show` 可见

#### Scenario: edit description only
- **WHEN** 用户 `senv ai provider edit api --description "内网网关"`
- **THEN** 仅说明更新，模型集与凭据引用不变

#### Scenario: model catalog text unchanged
- **WHEN** 档案说明被更新，某模型已有目录简介
- **THEN** 该模型简介保持原值

### Requirement: Provider 形态地址

LLM Provider SHALL 支持三个可选形态地址字段 `chat_base_url` / `responses_base_url` / `anthropic_base_url`，分别声明 openai-chat、openai-responses 与 anthropic 接口形态的接入地址；空值表示未声明。`senv ai provider add/edit` SHALL 通过重复 flag `--shape-url <api_shape>=<url>` 设置（`api_shape` 取值同 `--api-shape` 的合法枚举）：`add` 出现即设置；`edit` 传值设置、空值（`--shape-url <api_shape>=`）清空该字段、未出现的 key 保留原值。非法 key MUST 以非 0 退出且不写入。三字段的 URL 校验 SHALL 与 `BaseURL` 一致：默认仅接受 HTTPS、显式 `--allow-http` 后接受 HTTP、拒绝空 host 与 userinfo。归一化 SHALL 分族执行：`chat_base_url` 与 `responses_base_url` 沿用 `BaseURL` 的 OpenAI 兼容归一（收敛尾斜杠、补末段版本段、纯数字版本段视为已归一）；`anthropic_base_url` 原样存储、仅收敛尾斜杠，MUST NOT 追加、剥离或改写任何路径段（包括 `/v1`）。`BaseURL` SHALL 保持必填，语义为默认地址与推断源。存量档案 MUST NOT 要求迁移：字段缺省即旧行为。`show`/`list` SHALL 展示三个形态地址（未设显示 `-`），MUST NOT 输出凭据明文。

#### Scenario: 设置形态地址

- **WHEN** 用户执行 `senv ai provider add gw --base-url https://gw.example.com/v1 --shape-url anthropic=https://gw.example.com/api/anthropic ...`（其余必填项合法）
- **THEN** 档案保存 `anthropic_base_url` 为 `https://gw.example.com/api/anthropic`，`show` 展示该形态地址

#### Scenario: anthropic 形态地址原样存储

- **WHEN** 用户提供 `--shape-url anthropic=https://gw.example.com/api/anthropic/`（尾斜杠）或 `--shape-url anthropic=https://gw.example.com/api/anthropic/v1`
- **THEN** 前者收敛尾斜杠后存储；后者原样存储，MUST NOT 被剥去 `/v1` 或追加版本段

#### Scenario: OpenAI 族形态地址沿用归一

- **WHEN** 用户提供 `--shape-url responses=https://gw.example.com/responses-root`
- **THEN** 档案保存 `https://gw.example.com/responses-root/v1`，命令提示归一改写

#### Scenario: 非法 key 拒绝

- **WHEN** 用户传入 `--shape-url openai=https://gw.example.com`
- **THEN** 命令以非 0 退出并列出合法 key（openai-chat / openai-responses / anthropic），不写入任何变更

#### Scenario: edit 空值清空、省略保留

- **WHEN** 档案已设 `chat_base_url` 与 `anthropic_base_url`，用户执行 `edit <alias> --shape-url chat=` 且未出现 anthropic key
- **THEN** `chat_base_url` 从档案移除，`anthropic_base_url` 保持不变

#### Scenario: 校验与 HTTP 门禁覆盖形态地址

- **WHEN** 用户传入 `--shape-url anthropic=http://intranet.example.com/anthropic` 且未提供 `--allow-http`，或传入含 userinfo 的形态地址
- **THEN** 命令以非 0 退出，不写档案或凭据

#### Scenario: show/list 展示形态地址

- **WHEN** 档案设置了部分形态地址且用户执行 show 或 list
- **THEN** 输出包含已设形态地址，未设项显示 `-`，不含凭据明文
