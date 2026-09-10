## MODIFIED Requirements

### Requirement: 添加 LLM Provider 档案
`senv ai provider add` SHALL 以别名为唯一标识保存档案。凭据必须且只能通过交互式 TTY prompt 或 `--api-key-stdin` 提供；prompt 或 stdin 值 SHALL 只进入 vault 保留 text 组 `llm-keys/<alias>`，档案存引用 `text:llm-keys/<alias>`，且 MUST NOT 作为 CLI flag 值传入。也可用 `--key-ref` 指向既有 env/text entry（`env:<group>/<key>` 或 `text:<group>/<key>`）。base URL SHALL 默认仅接受 HTTPS；显式 `--allow-http` 后接受 HTTP；两种方案 MUST 拒绝 host 为空或包含 userinfo。base URL 落库前 SHALL 归一为 OpenAI 兼容形态：收敛路径尾斜杠，并在路径末段不为 `v1` 时追加 `/v1`；归一 MUST 保留 query 与 fragment，MUST NOT 改动中间路径段，且对已归一的输入保持原值。归一实际改动了输入时命令 SHALL 提示改写后的值。归一 MUST NOT 放宽既有校验：userinfo、空 host 与非允许的 HTTP 仍被拒绝。模型集为 `--catalog-provider` 指向的目录模型与 `--model` 自定义模型的并集且不得为空；`--default-model` 必须属于模型集。`--force` 覆盖档案时：未提供新自有凭据 SHALL 保留既有凭据与引用；从自有凭据改为外部引用 SHALL 删除原自有凭据；提供新自有凭据 SHALL 覆盖旧自有凭据。

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
