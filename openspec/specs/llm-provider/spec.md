# llm-provider Specification

## Purpose
把 LLM Provider 档案作为 vault 加密资产存储：档案与凭据分离（档案只存凭据引用），模型集可从模型目录自动装配并允许自定义，为后续 agent 切换提供唯一事实源。
## Requirements

### Requirement: 添加 LLM Provider 档案
`senv ai provider add` SHALL 以别名为唯一标识保存档案。凭据必须且只能通过交互式 TTY prompt 或 `--api-key-stdin` 提供；prompt 或 stdin 值 SHALL 只进入 vault 保留 text 组 `llm-keys/<alias>`，档案存引用 `text:llm-keys/<alias>`，且 MUST NOT 作为 CLI flag 值传入。也可用 `--key-ref` 指向既有 env/text entry（`env:<group>/<key>` 或 `text:<group>/<key>`）。base URL SHALL 默认仅接受 HTTPS；显式 `--allow-http` 后接受 HTTP；两种方案 MUST 拒绝 host 为空或包含 userinfo。base URL 落库前 SHALL 归一为 OpenAI 兼容形态：收敛路径尾斜杠，并在路径末段不为 `v1` 时追加 `/v1`；归一 MUST 保留 query 与 fragment，MUST NOT 改动中间路径段，且对已归一的输入保持原值。归一实际改动了输入时命令 SHALL 提示改写后的值。归一 MUST NOT 放宽既有校验：userinfo、空 host 与非允许的 HTTP 仍被拒绝。模型集为 `--catalog-provider` 指向的目录模型与 `--model` 自定义模型的并集且不得为空；每个模型 SHALL 解析 context window：显式 `--model-context <model>=<tokens>` 优先，其次档案既有元数据，最后 models.dev `limit.context`。add 时任一模型缺失 context window MUST 以非 0 退出且不写档案或凭据。`--default-model` 必须属于模型集。`--force` 覆盖档案时：未提供新自有凭据 SHALL 保留既有凭据与引用；从自有凭据改为外部引用 SHALL 删除原自有凭据；提供新自有凭据 SHALL 覆盖旧自有凭据。

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
- **WHEN** 用户执行 add 且给出 `--catalog-provider p1`、`--model custom-1`、`--model-context custom-1=1000000`，缓存中 p1 的 a、b 均有 `limit.context`
- **THEN** 档案保存成功，模型集为 a、b、custom-1，每个模型保存 context window/可用元数据，凭据按所选方式存入或引用，输出保存结果

#### Scenario: 模型缺少 context window
- **WHEN** add 的自定义模型没有 `--model-context`，且目录也没有对应 `limit.context`
- **THEN** 命令以非 0 退出并提示 `--model-context <model>=<tokens>`，不写档案或凭据

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
`senv ai provider list` SHALL 列出全部档案的摘要（别名、base_url、模型数、默认模型、目录来源、凭据引用）；`senv ai provider show` SHALL 展示单个档案详情，并在已保存模型元数据时展示各模型 context window。两者 MUST NOT 输出凭据明文。

#### Scenario: 列出档案
- **WHEN** vault 中存在档案且用户执行 list
- **THEN** 输出每个档案的摘要行且不含凭据明文

#### Scenario: 查看不存在的档案
- **WHEN** `show` 的别名不存在
- **THEN** 命令以非 0 退出并报错

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

### Requirement: 档案命令需要解锁 vault
`senv ai provider` 子命令 SHALL 在 vault 未初始化或未解锁时遵循既有认证流程（提示口令），不静默降级为明文存储。

#### Scenario: 未解锁时执行 add
- **WHEN** vault 已初始化但处于锁定状态且用户执行 add
- **THEN** 提示输入口令，认证通过后完成写入

### Requirement: 编辑 LLM Provider 档案
`senv ai provider edit <alias>` SHALL 支持修改 base_url、模型集、模型 context window、default_model、目录来源、凭据引用与 `api_shape`；alias 是主键 MUST NOT 被修改。凭据轮换语义 SHALL 与 `add` 一致：提供新自有凭据 SHALL 覆盖旧自有凭据；改为外部引用 SHALL 删除原自有凭据；未提供凭据来源 SHALL 保留既有凭据与引用。改动模型集、目录来源或模型 context window 时，context window 校验 SHALL 与 `add` 一致；仅修改其他字段时 MUST NOT 因旧档案缺少模型元数据而失败。校验（HTTPS 默认、拒绝 userinfo、模型集非空、`--default-model` 属于模型集、`--api-shape` 取值合法）SHALL 与 `add` 一致；任一步失败 MUST NOT 留下部分更新。TUI SHALL 通过同一方法提供等价编辑，别名在编辑态只读。

#### Scenario: 为旧档案补 context window
- **WHEN** 旧档案只有模型名，用户执行 `edit main --model-context m1=1000000`
- **THEN** 模型集保持不变，档案保存 m1 的 context window，后续切换无需 models.dev 缓存

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
