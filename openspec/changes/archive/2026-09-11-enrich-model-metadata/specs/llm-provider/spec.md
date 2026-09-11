## MODIFIED Requirements

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

### Requirement: 编辑 LLM Provider 档案
`senv ai provider edit <alias>` SHALL 支持修改 base_url、模型集、模型 context window、模型输出上限、模型推理档位、默认推理档、输入模态、default_model、目录来源、凭据引用与 `api_shape`；alias 是主键 MUST NOT 被修改。凭据轮换语义 SHALL 与 `add` 一致：提供新自有凭据 SHALL 覆盖旧自有凭据；改为外部引用 SHALL 删除原自有凭据；未提供凭据来源 SHALL 保留既有凭据与引用。改动模型集、目录来源、模型 context window、推理档位或默认推理档时，必填元数据校验 SHALL 与 `add` 一致；仅修改其他字段（含只补输出上限/输入模态）时 MUST NOT 因旧档案缺少模型元数据而失败。校验（HTTPS 默认、拒绝 userinfo、模型集非空、`--default-model` 属于模型集、`--api-shape` 取值合法、默认推理档属于档位列表）SHALL 与 `add` 一致；任一步失败 MUST NOT 留下部分更新。TUI SHALL 通过同一方法提供等价编辑，别名在编辑态只读。

#### Scenario: 为旧档案补 context window
- **WHEN** 旧档案只有模型名，用户执行 `edit main --model-context m1=1000000`
- **THEN** 模型集保持不变，档案保存 m1 的 context window，后续切换无需 models.dev 缓存

#### Scenario: 为旧档案补默认推理档
- **WHEN** 旧档案 m1 已有推理档位 `low;high` 但无默认推理档，用户执行 `edit main --model-default-reasoning m1=high`
- **THEN** 模型集与档位保持不变，档案保存 m1 的默认推理档为 `high`

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
