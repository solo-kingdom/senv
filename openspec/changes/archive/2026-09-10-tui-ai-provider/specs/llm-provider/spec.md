# llm-provider Delta

## ADDED Requirements

### Requirement: 编辑 LLM Provider 档案
`senv ai provider edit <alias>` SHALL 支持修改 base_url、模型集、default_model、目录来源、凭据引用与 `api_shape`；alias 是主键 MUST NOT 被修改。凭据轮换语义 SHALL 与 `add` 一致：提供新自有凭据 SHALL 覆盖旧自有凭据；改为外部引用 SHALL 删除原自有凭据；未提供凭据来源 SHALL 保留既有凭据与引用。校验（HTTPS 默认、拒绝 userinfo、模型集非空、`--default-model` 属于模型集、`--api-shape` 取值合法）SHALL 与 `add` 一致；任一步失败 MUST NOT 留下部分更新。TUI SHALL 通过同一方法提供等价编辑，别名在编辑态只读。

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
