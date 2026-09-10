# llm-provider Specification

## Purpose
把 LLM Provider 档案作为 vault 加密资产存储：档案与凭据分离（档案只存凭据引用），模型集可从模型目录自动装配并允许自定义，为后续 agent 切换提供唯一事实源。
## Requirements

### Requirement: 添加 LLM Provider 档案
`senv ai provider add` SHALL 以别名为唯一标识保存档案。凭据必须且只能通过交互式 TTY prompt 或 `--api-key-stdin` 提供；prompt 或 stdin 值 SHALL 只进入 vault 保留 text 组 `llm-keys/<alias>`，档案存引用 `text:llm-keys/<alias>`，且 MUST NOT 作为 CLI flag 值传入。也可用 `--key-ref` 指向既有 env/text entry（`env:<group>/<key>` 或 `text:<group>/<key>`）。base URL SHALL 默认仅接受 HTTPS；显式 `--allow-http` 后接受 HTTP；两种方案 MUST 拒绝 host 为空或包含 userinfo。模型集为 `--catalog-provider` 指向的目录模型与 `--model` 自定义模型的并集且不得为空；`--default-model` 必须属于模型集。`--force` 覆盖档案时：未提供新自有凭据 SHALL 保留既有凭据与引用；从自有凭据改为外部引用 SHALL 删除原自有凭据；提供新自有凭据 SHALL 覆盖旧自有凭据。

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

#### Scenario: force 保留既有自有凭据
- **WHEN** 已存在 alias 的自有凭据且用户用 `--force` 只更新模型或默认模型，不提供新凭据来源
- **THEN** 档案更新且原 vault 凭据保持不变

#### Scenario: force 改外部引用清理自有凭据
- **WHEN** 已存在自有凭据且用户用 `--force --key-ref env:llm/KEY` 更新档案
- **THEN** 档案引用外部 key，原 `llm-keys/<alias>` 条目被删除

#### Scenario: 目录模型加自定义模型
- **WHEN** 用户执行 add 且给出 `--catalog-provider p1` 与 `--model custom-1`，缓存中 p1 含模型 a、b
- **THEN** 档案保存成功，模型集为 a、b、custom-1，凭据按所选方式存入或引用，输出保存结果

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
`senv ai provider list` SHALL 列出全部档案的摘要（别名、base_url、模型数、默认模型、目录来源、凭据引用）；`senv ai provider show` SHALL 展示单个档案详情。两者 MUST NOT 输出凭据明文。

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
